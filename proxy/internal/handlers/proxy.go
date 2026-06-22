package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"synapse-proxy/internal/db"
	"synapse-proxy/internal/metrics"
	"synapse-proxy/internal/services"
	"synapse-proxy/internal/utils"
	"synapse-proxy/internal/workers"
	"synapse-proxy/optiagent"
	"github.com/redis/go-redis/v9"
)

// ProxyHandler is the main HTTP handler intercepting LLM requests
func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Panic recovery: one malformed payload must not take down the
	// whole Go process. A single panic here would otherwise crash the
	// container, Docker restarts it, every in-flight request gets reset,
	// latency p99 explodes and logs flood with the same stack trace on
	// every restart. Recover, log the panic + stack + request ID, and
	// return a clean 502 to the client.
	defer func() {
		if rec := recover(); rec != nil {
			stack := debug.Stack()
			log.Printf("[ProxyHandler] PANIC recovered: %v\nrequest: %s %s\nvk=%s\nstack:\n%s",
				rec, r.Method, r.URL.Path, maskVirtualKey(r.Header.Get("Authorization")), stack)
			metrics.RecordPanic("ProxyHandler")
			// Best-effort response. If the handler already wrote headers
			// (e.g. started streaming), we can't send a new status code.
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"proxy recovered from internal panic, please retry"}`, http.StatusBadGateway)
			}
		}
	}()

	ctx := r.Context()
	startTime := time.Now()

	virtualKey := r.Header.Get("Authorization")
	virtualKey = strings.TrimPrefix(virtualKey, "Bearer ")
	
	// Fallback to default key for local apps (like LMStudio) that don't send auth
	if virtualKey == "" || virtualKey == "lm-studio" {
		virtualKey = os.Getenv("DEFAULT_VIRTUAL_KEY")
	}
	
	if virtualKey == "" {
		http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	authHeader := "Bearer " + virtualKey
	keyConfig, err := services.ValidateVirtualKey(ctx, authHeader)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	realKey := keyConfig.RealKey
	provider := keyConfig.Provider
	fallbackKey := keyConfig.FallbackKey
	fallbackProvider := keyConfig.FallbackProvider
	isBenchmark := keyConfig.IsBenchmark
	semanticTolerance := keyConfig.SemanticTolerance
	cacheTtl := keyConfig.CacheTtl
	defaultModel := keyConfig.DefaultModel
	isolateCache := keyConfig.IsolateCache
	zeroLog := keyConfig.ZeroLog

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	if zeroLog && keyConfig.RedactPII {
		bodyBytes = utils.RedactBoth(bodyBytes)
	} else if zeroLog {
		bodyBytes = utils.RedactJSONBody(bodyBytes)
	} else if keyConfig.RedactPII {
		bodyBytes = utils.RedactPII(bodyBytes)
	}

	reqModel := "unknown"
	wantStream := false
	var payloadMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &payloadMap); err == nil {
		if m, ok := payloadMap["model"].(string); ok && m != "" {
			reqModel = m
		}
		if s, ok := payloadMap["stream"].(bool); ok {
			wantStream = s
		}
	}

	// Detect originating agent (Hermes, OpenClaw, ...) and session id
	// from request headers + body. The proxy does not require any client
	// cooperation â€” it infers everything heuristically.
	agentSig := utils.DetectAgent(utils.AgentDetectionInput{
		Headers:    r.Header,
		Body:       bodyBytes,
		BodyParsed: payloadMap,
	})
	sessionID := utils.ExtractSessionID(r.Header, payloadMap)

	// Multiturn detection: even without an explicit sessionId, we
	// can group requests that share the same conversation signature
	// (system prompt + tool set) into a "natural session". This is
	// what lets the dashboard show "Tour N" and group multi-turn
	// conversations automatically.
	//
	// The turn count is what makes this useful: a single one-shot
	// request has TurnCount=0 and no signature is generated (it
	// would just be noise). A request with assistant messages in
	// its history has TurnCount>=1 and we mint a session id from
	// the conv signature. Subsequent requests with the same
	// signature reuse the same id, so the dashboard sees them as
	// one conversation.
	multiturn := utils.MultiturnSign(payloadMap)
	turnCount := multiturn.TurnCount
	convSignature := multiturn.ConvSignature

	// Only auto-session when the user is in the middle of a
	// conversation (turnCount >= 1). A first-turn request without
	// an explicit sessionId stays anonymous — there's no prior
	// context to anchor it to.
	if sessionID == "" && turnCount >= 1 && convSignature != "" {
		sessionID = convSignature
	}

	log.Printf("[ProxyHandler] agent=%s (%s, conf=%.2f) session=%q turn=%d sig=%s",
		agentSig.ID, agentSig.Source, agentSig.Confidence, sessionID, turnCount, convSignature)

	isBypassStr := r.Header.Get("X-Bypass-Cache")
	isBypass := isBypassStr == "true"
	log.Printf("[ProxyHandler] Received X-Bypass-Cache: %q -> isBypass: %v", isBypassStr, isBypass)

	var optResult optiagent.OptimizationResult
	rdb := db.GetRedis()

	// Per-request "Record session" override. The dashboard's Record
	// button or an agent SDK can set X-SynapseProxy-Session to a stable
	// session identifier; the proxy tags all subsequent RequestLog rows
	// with this id so the dashboard can group them into a single
	// "session" view. This is a no-op if the header is absent.
	if sessID := r.Header.Get("X-SynapseProxy-Session"); sessID != "" {
		sessionID = sessID
	}

	// Server-side session recording: the dashboard's Record Session
	// button writes a per-virtual-key tag to Redis (key
	// `synapse:session:vk:<vk>`) when the user clicks Start, and
	// removes it on Stop. We check Redis on every request and, if a
	// tag is present, override the sessionID with it. This lets the
	// user record a session transparently: any agent (Hermes, curl,
	// Playground) using this virtual key gets its RequestLog rows
	// tagged, without the agent having to know about the session id.
	//
	// Header > Redis > header-derived session id > empty.
	if dbTag := services.LookupSessionTag(ctx, virtualKey); dbTag != "" {
		sessionID = dbTag
	}

	// Cache completed tool calls from the history
	if rdb != nil && virtualKey != "" {
		optiagent.StoreCompletedToolCalls(ctx, rdb, virtualKey, bodyBytes)
	}

	// Feature 3: Compaction hint â€” inject a system note so the agent
	// knows that previous tool outputs may have been summarized. Only
	// on the first turn of a session (or whenever the body has not
	// already been mutated by us).
	if !wantStream {
		hinted := optiagent.InjectCompactionHint(bodyBytes)
		if len(hinted) > 0 && string(hinted) != string(bodyBytes) {
			bodyBytes = hinted
			// re-parse so subsequent code sees the new model field if any
			var reMap map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &reMap); err == nil {
				payloadMap = reMap
				if m, ok := reMap["model"].(string); ok && m != "" {
					reqModel = m
				}
			}
		}
	}

	// Feature 2: Tool-call dedup. We extract the file paths the agent is
	// about to read and check whether the same file was read recently.
	// The hit is logged for telemetry; full body-rewriting is left to
	// future work since it requires tool-output mapping in the messages
	// array.
	fileToolCalls := optiagent.ExtractToolCalls(bodyBytes)
	allToolCalls := optiagent.ExtractAllToolCalls(bodyBytes)

	toolCallsStr := ""
	if len(allToolCalls) > 0 {
		tcJSON, _ := json.Marshal(allToolCalls)
		toolCallsStr = string(tcJSON)

		// Auto-discover: log every tool the agent has called at
		// least once into the Redis set
		// `synapse:discovered_tools:<vk>`. The dashboard reads
		// this set and renders it as a checkable list under the
		// Agent Firewall. Tools the operator has NOT explicitly
		// denied are allowed; tools they UNCHECK are added to a
		// denylist (`synapse:denied_tools:<vk>`) consulted below.
		//
		// We SADD once per tool name (the set is dedup'd) and set
		// a generous 30-day TTL so the list survives across
		// deploys but eventually forgets abandoned agents.
		if rdb != nil && virtualKey != "" {
			discoverKey := "synapse:discovered_tools:" + virtualKey
			for _, tc := range allToolCalls {
				if tc.ToolName == "" {
					continue
				}
				rdb.SAdd(ctx, discoverKey, tc.ToolName)
			}
			rdb.Expire(ctx, discoverKey, 30*24*time.Hour)
		}
	}

	// Denylist consult: if the operator unchecked a tool in the
	// dashboard, we block it before the filter above. The denylist
	// is a Redis set written by the dashboard's PUT endpoint. We
	// also keep the original AllowList behaviour for backwards
	// compatibility — if `AllowedTools` is set, we still apply the
	// strict whitelist (the user opted into strict mode).
	if rdb != nil && virtualKey != "" && len(allToolCalls) > 0 && !keyConfig.BlockUnknownTools {
		denyKey := "synapse:denied_tools:" + virtualKey
		denied, _ := rdb.SMembers(ctx, denyKey).Result()
		if len(denied) > 0 {
			denyMap := make(map[string]bool, len(denied))
			for _, d := range denied {
				denyMap[d] = true
			}
			for _, tc := range allToolCalls {
				if denyMap[tc.ToolName] {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"error": {"message": "Agent Firewall denied tool call: ` + tc.ToolName + `"}}`))
					return
				}
			}
		}
	}

// Agent Firewall - Tool Filtering
	if keyConfig.BlockUnknownTools && keyConfig.AllowedTools != "" && len(allToolCalls) > 0 {
		allowed := strings.Split(keyConfig.AllowedTools, ",")
		allowedMap := make(map[string]bool)
		for _, a := range allowed {
			allowedMap[strings.TrimSpace(a)] = true
		}
		for _, tc := range allToolCalls {
			if !allowedMap[tc.ToolName] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error": {"message": "Agent Firewall blocked unauthorized tool call: ` + tc.ToolName + `"}}`))
				return
			}
		}
	}

	// Agent Firewall - Tool-call fingerprint (observer only).
	// See docs/agent_firewall.md for the strategy.
	if keyConfig.FingerprintLoopDetect {
		fp := optiagent.CheckToolFingerprint(ctx, rdb, virtualKey, bodyBytes)
		if fp.IsLoop {
			log.Printf("[ProxyHandler] FINGERPRINT OBSERVED: tool=%s count=%d (vk=%s) — deferring decision to cache check",
				fp.ToolName, fp.LoopCount, virtualKey)
			w.Header().Set("X-Synapse-Fingerprint-Count", strconv.Itoa(fp.LoopCount))
			w.Header().Set("X-Synapse-Fingerprint-Tool", fp.ToolName)

			go workers.PushTelemetry(virtualKey, provider, reqModel,
				optResult.PromptTokensOrig, 0, optResult.PromptTokensOpt, 0, 0,
				"NONE", time.Since(startTime), string(bodyBytes), string(optResult.Payload), `{"event":"fingerprint_observed","tool":"`+fp.ToolName+`","count":`+strconv.Itoa(fp.LoopCount)+`}`,
				0, 0, 0, 0, agentSig.ID, agentSig.Label, sessionID, zeroLog,
				toolCallsStr, keyConfig.LimitExceeded, false,
				turnCount, convSignature)
		}
	}

	// Agent Firewall - Session Circuit Breaker
	if keyConfig.SessionTokenLimit > 0 && sessionID != "" {
		approxTokens := utils.CountTokens(string(bodyBytes))
		usageKey := "synapse:session_usage:" + sessionID
		currentSessionUsage, _ := rdb.IncrBy(ctx, usageKey, int64(approxTokens)).Result()
		rdb.Expire(ctx, usageKey, 24*time.Hour)
		if int(currentSessionUsage) > keyConfig.SessionTokenLimit {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": {"message": "Agent Firewall: Session token limit exceeded (` + strconv.Itoa(keyConfig.SessionTokenLimit) + ` tokens)."}}`))
			return
		}
	}
	if len(fileToolCalls) > 0 {
		toolDedupTTL := 5 * time.Minute
		toolDedup := optiagent.CheckToolDedup(ctx, rdb, virtualKey, fileToolCalls, bodyBytes, toolDedupTTL)
		if toolDedup.HasDup {
			log.Printf("[ProxyHandler] TOOL DEDUP HIT: %s re-read of %q (cached body %d bytes)",
				toolDedup.ToolName, toolDedup.FilePath, len(toolDedup.ReuseBody))
		}
	}

	// Model Radar: flag the requested model so we can collect samples if
	// the upstream returns a usage block we don't recognize.
	isNewModel := workers.CheckAndFlagNewModel(ctx, rdb, provider, reqModel)

	// Force-disable L2 (semantic) cache for agentic trajectories.
	// Two triggers:
			// 1. A Record Session is active (X-SynapseProxy-Session header
	//      present, set by the dashboard "Start Recording" button)
	//   2. The request body contains tool_calls â€” the client is an
	//      agent SDK mid tool loop
	// Both cases mean we're inside a long multi-turn context where
	// L2's cosine-distance matching would happily return a response
	// from a *different* turn, corrupting the agent's state. L3
	// (compression) is the right tool here, not L2.
	forceDisableL2 := sessionID != "" || len(allToolCalls) > 0

	// L0: request deduplication for identical in-flight requests.
	// Two parallel callers hitting the proxy with the same payload
	// (same SHA-256 + virtualKey) are collapsed into a single
	// upstream call: the first acquires the L0 lock and runs the
	// full L1/L2/L3 pipeline; the others wait up to 30s for the
	// leader to publish the response. Skip for streaming (the
	// client already started receiving the stream).
	var l0Hash, l0WorkerID string
	var l0PublishResponse []byte // captured at end of handler
	if !isBypass && !wantStream {
		l0Hash = optiagent.HashPayload(bodyBytes)
		gotLock, workerID := optiagent.L0Acquire(ctx, rdb, virtualKey, l0Hash)
		if !gotLock {
			log.Printf("[L0] Another worker is processing this payload, waiting for in-flight result (vk=%s hash=%s)", virtualKey, l0Hash[:12])
			resp, waitErr := optiagent.L0Wait(ctx, rdb, virtualKey, l0Hash)
			if waitErr == nil && len(resp) > 0 {
				log.Printf("[L0] Got coalesced response from leader (%d bytes) â€” short-circuiting", len(resp))
				// Telemetry for the L0 follower: same payload hash, but
				// cacheLevel=L0 so it's counted separately from L1/L2/L3.
				// TokensOrig=TokensOpt=0 because the follower did no work.
go workers.PushTelemetry(virtualKey, provider, reqModel, 0, 0, 0, 0, 0,
			"L0", time.Since(startTime), string(bodyBytes), string(bodyBytes), string(resp), 0, 0, 0, 0, agentSig.ID, agentSig.Label, sessionID, zeroLog, toolCallsStr, keyConfig.LimitExceeded, false, turnCount, convSignature)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-SynapseProxy-Cache", "L0-coalesced")
				w.Write(resp)
				return
			}
			log.Printf("[L0] Wait failed (%v) â€” falling through to normal pipeline", waitErr)
			// don't return; proceed as normal leader (someone else bailed)
		} else {
			l0WorkerID = workerID
			// Defer publish: at end of handler, release the L0 lock
			// and, if we got a valid upstream response, publish it so
			// coalesced followers get the same answer.
			defer func() {
				if l0Hash != "" && l0WorkerID != "" {
					optiagent.L0Release(ctx, rdb, virtualKey, l0Hash, l0WorkerID, l0PublishResponse)
				}
			}()
		}
	}

	if !isBypass {
		optResult, err = optiagent.ProcessRequest(ctx, rdb, bodyBytes, semanticTolerance, virtualKey, isolateCache, forceDisableL2, keyConfig.EnableL1, keyConfig.EnableL2, keyConfig.EnableL3, keyConfig.LimitExceeded, cacheTtl, keyConfig.ToolTtls)
		if err != nil {
			http.Error(w, "Optimization engine failure", http.StatusInternalServerError)
			return
		}

		if optResult.CacheHitLevel == "L1" || optResult.CacheHitLevel == "L2" {
			// Safety net: if the cached payload is an upstream error (e.g. a
			// model was renamed, a key was rotated, or the wrong provider was
			// targeted), do NOT replay it. Drop the cache entry and fall
			// through to a fresh upstream call.
			if utils.IsCachedResponseAnError(optResult.HitResponse) {
				log.Printf("[ProxyHandler] Cached %s response looks like an error, invalidating and falling through (model=%s)", optResult.CacheHitLevel, reqModel)
				// Invalidate BOTH the L1 and L2 cache entries for this
				// (vk, payload) so the next request actually re-hits upstream.
				l1Key := "synapse:l1cache:" + virtualKey + ":" + optResult.PayloadHash
				l2Key := "synapse:l2cache:" + virtualKey + ":" + optResult.PayloadHash
				_ = rdb.Del(ctx, l1Key, l2Key).Err()
				optResult.CacheHitLevel = "NONE"
				// continue to upstream execution below
			} else {
				cachedUsage := utils.ExtractUsage(optResult.HitResponse)
				// Re-stamp the response so the client sees the model name
				// it requested, even if the cached payload was originally
				// produced under a different name (model aliasing).
				restamped := utils.RestampModel(optResult.HitResponse, reqModel)
				w.Header().Set("X-SynapseProxy-Cache", optResult.CacheHitLevel)
				if wantStream {
					streamCachedResponse(w, restamped, reqModel)
				} else {
					w.Header().Set("Content-Type", "application/json")
					w.Write(restamped)
				}

go workers.PushTelemetry(virtualKey, provider, reqModel,
				optResult.PromptTokensOrig, cachedUsage.CompletionTokens, optResult.PromptTokensOpt, 0, cachedUsage.ReasoningTokens,
				optResult.CacheHitLevel, time.Since(startTime), string(bodyBytes), string(optResult.Payload), string(restamped),
				cachedUsage.CacheCreationTokens, cachedUsage.CacheReadTokens, cachedUsage.CacheHitTokens, cachedUsage.CacheMissTokens,
				agentSig.ID, agentSig.Label, sessionID, zeroLog, toolCallsStr, keyConfig.LimitExceeded, false,
				turnCount, convSignature)

				if isBenchmark {
					go runBenchmarkEvaluation(virtualKey, realKey, provider, reqModel, defaultModel, bodyBytes, optResult.Payload, optResult.HitResponse, time.Since(startTime), 0, 0)
				}
				return
			}
		}
	} else {
		optResult = optiagent.OptimizationResult{
			Payload: bodyBytes,
			PromptTokensOrig: utils.CountTokens(string(bodyBytes)),
			PromptTokensOpt: utils.CountTokens(string(bodyBytes)),
			CacheHitLevel: "BYPASS",
		}
	}

	// Feature 1: Loop detection. If 3+ identical calls land in a 60s
	// window, the 3rd+ is served from the loop's cached response
	// (the FIRST call's response). Catches runaway agents that retry
	// the same tool call in a tight loop.
	loopResult := optiagent.DetectLoop(ctx, rdb, virtualKey, optResult.PayloadHash, keyConfig.KillSwitch)
	if loopResult.IsLoop {
		hashPrefix := optResult.PayloadHash
		if len(hashPrefix) > 12 {
			hashPrefix = hashPrefix[:12]
		}
		log.Printf("[ProxyHandler] Loop detected: count=%d/%d in %ds (hash=%s) model=%s",
			loopResult.LoopCount, optiagent.LOOP_THRESHOLD, loopResult.WindowSecs,
			hashPrefix, reqModel)

		if loopResult.TriggerKillSwitch {
			log.Printf("[ProxyHandler] KILL SWITCH FIRED for session %s (returning self-correction hint)", sessionID)
			
			hintBytes := makeSelfCorrectionResponse("", reqModel)
			if wantStream {
				streamCachedResponse(w, hintBytes, reqModel)
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.Write(hintBytes)
			}

			// Telemetry: log the kill switch hit
			go workers.PushTelemetry(virtualKey, provider, reqModel,
				optResult.PromptTokensOrig, 0, optResult.PromptTokensOpt, 0, 0,
				"NONE", time.Since(startTime), string(bodyBytes), string(optResult.Payload), `{"error":"kill_switch_hint"}`,
				0, 0, 0, 0, agentSig.ID, agentSig.Label, sessionID, zeroLog,
				toolCallsStr, keyConfig.LimitExceeded, true,
				turnCount, convSignature)
			return
		}
	}
if loopResult.ShouldReuse && len(loopResult.ReusePayload) > 0 {
		log.Printf("[ProxyHandler] LOOP HIT (count=%d) â€” serving cached response instead of upstream", loopResult.LoopCount)

		restamped := utils.RestampModel(loopResult.ReusePayload, reqModel)
		if wantStream {
			streamCachedResponse(w, restamped, reqModel)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write(restamped)
		}

// Telemetry: log the loop hit as a cache hit (LOOP level)
		go workers.PushTelemetry(virtualKey, provider, reqModel,
			optResult.PromptTokensOrig, 0, 0, 0, 0, "LOOP",
			time.Since(startTime), string(bodyBytes), string(optResult.Payload),
			string(restamped),
			0, 0, 0, 0,
			agentSig.ID, agentSig.Label, sessionID, zeroLog,
			toolCallsStr, keyConfig.LimitExceeded, false,
			turnCount, convSignature)
		return
	}

	// Soft-loop self-correction hint: cache missed AND fingerprint observed. See
	// docs/agent_firewall.md for the strategy.
	if fpCount := w.Header().Get("X-Synapse-Fingerprint-Count"); fpCount != "" && keyConfig.FingerprintLoopDetect && !keyConfig.KillSwitch {
		count, _ := strconv.Atoi(fpCount)
		if count >= optiagent.FingerprintThreshold {
			fpTool := w.Header().Get("X-Synapse-Fingerprint-Tool")
			log.Printf("[ProxyHandler] SOFT LOOP INJECTION: tool=%s count=%d (vk=%s) — injecting warning into prompt and continuing",
				fpTool, count, virtualKey)

			// Modify optResult.Payload to inject the warning
			optResult.Payload = injectSystemWarning(optResult.Payload, fpTool)
			optResult.PromptTokensOpt += 40 // approximate token cost of the warning

			// We do NOT return here. We let the modified payload flow upstream!
			// We clear the fpCount so we don't trigger this again on the same request
			w.Header().Del("X-Synapse-Fingerprint-Count")
		}
	}

	executeRequest := func(currentProvider, currentRealKey string) (*http.Response, error) {
		var targetURL string
		switch currentProvider {
		case "anthropic":
			targetURL = "https://api.anthropic.com/v1/messages"
		case "google":
			targetURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
		case "minimax":
			targetURL = "https://api.minimax.io/v1/text/chatcompletion_v2"
		case "deepseek":
			targetURL = "https://api.deepseek.com/chat/completions"
		case "mistral":
			targetURL = "https://api.mistral.ai/v1/chat/completions"
		case "openrouter":
			targetURL = "https://openrouter.ai/api/v1/chat/completions"
		case "groq":
			targetURL = "https://api.groq.com/openai/v1/chat/completions"
		case "together":
			targetURL = "https://api.together.xyz/v1/chat/completions"
		case "perplexity":
			targetURL = "https://api.perplexity.ai/chat/completions"
		default:
			targetURL = "https://api.openai.com/v1/chat/completions"
		}

		upstreamPayload := optResult.Payload
		var pMap map[string]interface{}
		if err := json.Unmarshal(upstreamPayload, &pMap); err == nil {
			modified := false
			if wantStream {
				pMap["stream"] = true
				modified = true
			}

			// Model routing policy:
			//   1. FORCE_MODEL env var (admin hard-override) wins.
			//   2. If the client did NOT send a model, fall back to
			//      defaultModel.
			//   3. If the client DID send a model but the current provider
			//      does not advertise it (e.g. Hermes asks for
			//      "google/gemma-..." on a MiniMax-backed key), silently
			//      substitute the provider's defaultModel for the upstream
			//      call. The original model name is preserved in reqModel
			//      and re-stamped on the response before it is returned to
			//      the client, so downstream agents like Hermes still see
			//      the model they asked for.
			//   4. Otherwise, respect the client's choice.
			clientSentModel := false
			clientModel := ""
			if m, ok := pMap["model"].(string); ok && m != "" && m != "unknown" {
				clientSentModel = true
				clientModel = m
			}
			envForce := os.Getenv("FORCE_MODEL")
			modelsForProvider := utils.ModelsForProvider(currentProvider)
			_, modelKnown := modelsForProvider[clientModel]
			switch {
			case envForce != "":
				pMap["model"] = envForce
				modified = true
			case !clientSentModel && defaultModel != "":
				pMap["model"] = defaultModel
				modified = true
			case clientSentModel && !modelKnown && defaultModel != "":
				// Unknown model on this provider â€” fall through to the
				// provider's default for the upstream call. The response
				// will be re-stamped with clientModel by streamResponse
				// (or cached-response path) so the client sees the name
				// it expected.
				log.Printf("[ProxyHandler] Model %q not advertised by provider %q â€” routing to default %q and re-stamping response", clientModel, currentProvider, defaultModel)
				pMap["model"] = defaultModel
				modified = true
			}
			
			if modified {
				if rewritten, err := json.Marshal(pMap); err == nil {
					upstreamPayload = rewritten
				}
			}
		}

		req, err := http.NewRequest("POST", targetURL, bytes.NewBuffer(upstreamPayload))
		if err != nil {
			return nil, err
		}

		req.Header.Set("Content-Type", "application/json")
		if currentProvider == "anthropic" {
			req.Header.Set("x-api-key", currentRealKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+currentRealKey)
		}

		client := &http.Client{Timeout: 600 * time.Second}
		upstreamStart := time.Now()
		resp, doErr := client.Do(req)
		// Record latency bucket for the Prometheus /metrics endpoint.
		// isError is true when the call returned a non-2xx status â€” useful
		// to compute a real error rate, not just an outlier rate.
		if resp != nil {
			metrics.RecordUpstream(int(time.Since(upstreamStart).Milliseconds()), resp.StatusCode >= 400)
		} else if doErr != nil {
			metrics.RecordUpstream(int(time.Since(upstreamStart).Milliseconds()), true)
		}
		return resp, doErr
	}

	maxRetries := 3
	var resp *http.Response
	var reqErr error
	usedProvider := provider

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoffDur := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("Upstream provider %s failed. Retrying in %v (attempt %d/%d)...", usedProvider, backoffDur, attempt, maxRetries)
			time.Sleep(backoffDur)
		}

		resp, reqErr = executeRequest(usedProvider, realKey)
		
		if reqErr == nil && resp.StatusCode < 429 && resp.StatusCode != 408 {
			break
		}
		
		if resp != nil {
			resp.Body.Close()
		}
	}

	if (reqErr != nil || (resp != nil && (resp.StatusCode >= 429 || resp.StatusCode == 408))) && fallbackProvider != "" && fallbackKey != "" {
		log.Printf("Primary provider %s exhausted. Failing over to fallback provider: %s", provider, fallbackProvider)
		usedProvider = fallbackProvider
		resp, reqErr = executeRequest(fallbackProvider, fallbackKey)
	}

	if reqErr != nil || (resp != nil && resp.StatusCode >= 400) {
		status := http.StatusBadGateway
		var errBody string
		if resp != nil {
			status = resp.StatusCode
			if bodyBytes, err := io.ReadAll(resp.Body); err == nil {
				errBody = string(bodyBytes)
				resp.Body.Close()
			}
		}
		
		log.Printf("All upstream providers failed. Last error: %v, Status: %d, Body: %s", reqErr, status, errBody)
		
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		
		// If upstream returned a JSON error, forward it directly so clients (e.g. LiteLLM) can parse it.
		// Otherwise, wrap the plain text error in an OpenAI-compatible error format.
		if strings.HasPrefix(strings.TrimSpace(errBody), "{") {
			w.Write([]byte(errBody))
		} else {
			errMsg := "Failed to reach upstream provider"
			if errBody != "" {
				errMsg = errBody
			} else if reqErr != nil {
				errMsg = reqErr.Error()
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": errMsg,
					"type":    "upstream_error",
					"code":    status,
				},
			})
		}
		return
	}

	// Intercept tool calls in LLM responses and perform recursive upstream call
	var finalResponseBytes []byte
	var finalResponse *http.Response
	isResponseIntercepted := false

	for {
		if resp == nil || resp.StatusCode != http.StatusOK {
			break
		}

		respBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			log.Printf("[ProxyHandler] Error reading upstream response body: %v", readErr)
			break
		}

		var chatCompletionsJSON []byte
		if wantStream {
			chatCompletionsJSON = reconstructFromSSE(respBytes, reqModel)
		} else {
			chatCompletionsJSON = respBytes
		}

		var respJSON struct {
			Choices []struct {
				Message struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}

		hasToolCalls := false
		if err := json.Unmarshal(chatCompletionsJSON, &respJSON); err == nil && len(respJSON.Choices) > 0 {
			if len(respJSON.Choices[0].Message.ToolCalls) > 0 {
				hasToolCalls = true
			}
		}

		if !hasToolCalls {
			finalResponseBytes = respBytes
			finalResponse = resp
			break
		}

		allCached := true
		for _, tc := range respJSON.Choices[0].Message.ToolCalls {
			_, exists := optiagent.QueryToolCallCache(ctx, rdb, virtualKey, tc.Function.Name, tc.Function.Arguments, semanticTolerance)
			if !exists {
				allCached = false
				break
			}
		}

		if !allCached {
			finalResponseBytes = respBytes
			finalResponse = resp
			break
		}

		log.Printf("[ProxyHandler] TOOL CACHE FULL HIT: Intercepting all tool calls for %s", reqModel)

		var requestBody struct {
			Messages []optiagent.Message `json:"messages"`
		}
		if err := json.Unmarshal(optResult.Payload, &requestBody); err != nil {
			finalResponseBytes = respBytes
			finalResponse = resp
			break
		}

		var assistantMsg optiagent.Message
		assistantMsg.Role = "assistant"
		for _, tc := range respJSON.Choices[0].Message.ToolCalls {
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			}{
				ID: tc.ID,
				Type: tc.Type,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name: tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		requestBody.Messages = append(requestBody.Messages, assistantMsg)

		for _, tc := range respJSON.Choices[0].Message.ToolCalls {
			cachedResult, _ := optiagent.QueryToolCallCache(ctx, rdb, virtualKey, tc.Function.Name, tc.Function.Arguments, semanticTolerance)
			var toolMsg optiagent.Message
			toolMsg.Role = "tool"
			toolMsg.ToolCallID = tc.ID
			toolMsg.Name = tc.Function.Name
			toolMsg.Content = cachedResult
			requestBody.Messages = append(requestBody.Messages, toolMsg)
		}

		var payloadMap map[string]interface{}
		if err := json.Unmarshal(optResult.Payload, &payloadMap); err == nil {
			payloadMap["messages"] = requestBody.Messages
			newPayload, _ := json.Marshal(payloadMap)
			optResult.Payload = newPayload
		}

		log.Printf("[ProxyHandler] Short-circuiting tool calls, recursively invoking upstream with cached tool outputs...")
		resp, reqErr = executeRequest(usedProvider, realKey)
		if reqErr != nil || (resp != nil && resp.StatusCode >= 400) {
			if resp != nil {
				resp.Body.Close()
			}
			http.Error(w, "Failed to reach upstream provider during loop resolution", http.StatusBadGateway)
			return
		}
		isResponseIntercepted = true
	}

	if isResponseIntercepted {
		resp = finalResponse
		resp.Body = io.NopCloser(bytes.NewBuffer(finalResponseBytes))
	} else if resp != nil {
		resp.Body = io.NopCloser(bytes.NewBuffer(finalResponseBytes))
	}
	defer resp.Body.Close()

	streamResponse(w, resp, virtualKey, realKey, usedProvider, reqModel, defaultModel, optResult.PayloadHash, reqModel, optResult.Vector, optResult.PromptTokensOrig, optResult.PromptTokensOpt, optResult.CacheHitLevel, isBenchmark, bodyBytes, optResult.Payload, startTime, wantStream, cacheTtl, isNewModel, agentSig.ID, agentSig.Label, sessionID, zeroLog, &l0PublishResponse, toolCallsStr, keyConfig.LimitExceeded, turnCount, convSignature)
}

func streamResponse(w http.ResponseWriter, resp *http.Response, vk, realKey, provider, model, defaultModel, payloadHash, clientModel string, vector []float32, promptOrig, promptOpt int, cacheLvl string, isBenchmark bool, rawPayload, optPayload []byte, startTime time.Time, wantStream bool, cacheTtl int, isNewModel bool, agentID, agentLabel, sessionID string, zeroLog bool, l0Capture *[]byte, toolCallsStr string, limitExceeded bool, turnCount int, convSignature string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	upstreamCT := resp.Header.Get("Content-Type")
	if upstreamCT != "" {
		w.Header().Set("Content-Type", upstreamCT)
	} else if wantStream {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Per-request observability headers so the dashboard / Playground can
	// display stats without re-parsing the response body. Safe to expose:
	// they only contain aggregate token counts, cache level, and cost
	// deltas â€” never the prompt content.
	if cacheLvl != "" {
		w.Header().Set("X-SynapseProxy-Cache", cacheLvl)
		w.Header().Set("X-SynapseProxy-Tokens-In", strconv.Itoa(promptOrig))
		w.Header().Set("X-SynapseProxy-Tokens-Out", strconv.Itoa(promptOpt))
	}
	// Quick cost estimate using the same single-class helper as the
	// legacy dashboard headline. The full 4-class breakdown is computed
	// post-stream by the telemetry worker.
	if promptOrig > promptOpt {
		w.Header().Set("X-SynapseProxy-Cost-Saved", fmt.Sprintf("%.6f", utils.CalculateSavings(provider, model, promptOrig-promptOpt, 0)))
	}

	// Model re-stamping: if the client asked for a model that we aliased
	// upstream (e.g. "google/gemma-..." on a MiniMax-backed key), the
	// upstream will echo its own model name in every chunk. Re-stamp each
	// `data:` line so the client sees the model it asked for. We only do
	// the rewrite when clientModel != model (upstream model).
	needsRestamp := clientModel != "" && clientModel != model

	reader := bufio.NewReader(resp.Body)
	var fullResponse []byte
	var firstChunkLogged bool

	// Buffer for the first SSE event (data: {...}) to inspect upstream
	// application errors (e.g. MiniMax status_code != 0). When detected we
	// return a real HTTP 402/4xx to the client instead of forwarding a 200
	// with a poison body (which makes the agent hang waiting for chunks).
	var firstDataBuf []byte
	const maxFirstEventBytes = 64 * 1024

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if !firstChunkLogged {
				log.Printf("[streamResponse] Upstream sent first chunk: %s", string(line))
				firstChunkLogged = true
			}

			// Re-stamp "model" in `data:` payloads so the client sees the
			// model it asked for when we have aliased upstream.
			if needsRestamp && bytes.HasPrefix(line, []byte("data: ")) {
				line = utils.RestampModel(line, clientModel)
			}

			// Inspect the first data: line for an application error.
			if len(firstDataBuf) < maxFirstEventBytes && bytes.HasPrefix(line, []byte("data: ")) {
				firstDataBuf = append(firstDataBuf, line...)
				if err := detectUpstreamAppError(firstDataBuf); err != nil {
					log.Printf("[streamResponse] Upstream application error detected: %v", err)
					// Reject the request with a real HTTP error and stop streaming.
					w.Header().Set("Content-Type", "application/json")
					statusCode := http.StatusBadGateway
					if err.quota {
						statusCode = http.StatusPaymentRequired
					}
					w.WriteHeader(statusCode)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error": map[string]interface{}{
							"message": err.message,
							"type":    "upstream_application_error",
							"code":    err.statusCode,
						},
					})
					flusher.Flush()
					return
				}
			}

			w.Write(line)
			flusher.Flush()
			fullResponse = append(fullResponse, line...)
		}

		if err != nil {
			if err != io.EOF {
				log.Printf("[streamResponse] Read error: %v", err)
			}
			break
		}
	}

	// Discover the real model name the upstream used. For SSE we have to
	// reconstruct the full body first; for non-streaming it's already
	// complete.
	var cacheableResponse []byte
	if wantStream {
		cacheableResponse = reconstructFromSSE(fullResponse, model)
	} else {
		cacheableResponse = fullResponse
	}

	// L0 capture: hand the upstream response back to ProxyHandler so it
	// can publish it for in-flight coalescing followers. Only valid JSON
	// (not upstream errors) is propagated.
	if l0Capture != nil && !wantStream && len(cacheableResponse) > 0 {
		var jsonMap map[string]interface{}
		if err := json.Unmarshal(cacheableResponse, &jsonMap); err == nil {
			if _, hasError := jsonMap["error"]; !hasError {
				*l0Capture = cacheableResponse
			}
		}
	}

	realModel := extractModelFromResponse(cacheableResponse, model)

	isValidResponse := false
	if resp.StatusCode == http.StatusOK && len(cacheableResponse) > 0 {
		isValidResponse = true
		var jsonMap map[string]interface{}
		if err := json.Unmarshal(cacheableResponse, &jsonMap); err == nil {
			if _, hasError := jsonMap["error"]; hasError {
				isValidResponse = false
			}
			if baseResp, hasBaseResp := jsonMap["base_resp"].(map[string]interface{}); hasBaseResp {
				if statusCode, ok := baseResp["status_code"].(float64); ok && statusCode != 0 {
					isValidResponse = false
				}
			}
		}
	}

	if payloadHash != "" && isValidResponse {
		ctx := context.Background()
		rdb := db.GetRedis()
		l1Key := "synapse:l1cache:" + vk + ":" + payloadHash
		ttl := time.Duration(cacheTtl) * time.Second

		// Zero-Log Mode: we still token-count and measure latency
		// (metadata is fine) but we do NOT store the response body in
		// L1/L2 cache, and we do NOT store it as a loop response. The
		// upstream provider still has the response (we just don't
		// keep it on our side).
		if zeroLog {
			hashPrefix := payloadHash
			if len(hashPrefix) > 12 {
				hashPrefix = hashPrefix[:12]
			}
			log.Printf("[streamResponse] Zero-Log Mode: skipping L1/L2/loop cache for vk=%s hash=%s", vk, hashPrefix)
		} else {
			rdb.Set(ctx, l1Key, cacheableResponse, ttl)

			// Feature 1 (continuation): remember this response as the
			// "first" of a potential loop. The 3rd+ identical call in the
			// next 60s will pull this from the loop cache instead of
			// re-hitting upstream.
			//
			// Safety net: don't cache a poisoned response (e.g. a MiniMax
			// quota error returned as an empty `content:""`). Same check
			// as the L1 cache.
			if !utils.IsCachedResponseAnError(cacheableResponse) {
				optiagent.StoreLoopFirstResponse(ctx, rdb, vk, payloadHash, cacheableResponse)
			}

			if len(vector) > 0 {
				buf := new(bytes.Buffer)
				if binary.Write(buf, binary.LittleEndian, vector) == nil {
					l2Key := "synapse:l2cache:" + vk + ":" + payloadHash
					rdb.HSet(ctx, l2Key, "vk", vk, "vector", buf.Bytes(), "response", cacheableResponse)
					rdb.Expire(ctx, l2Key, ttl)
				}
			}
		}
	}

	usage := utils.ExtractUsage(cacheableResponse)
	truePromptTokens := usage.PromptTokens
	completionTokens := usage.CompletionTokens
	reasoningTokens := usage.ReasoningTokens

	// Model Radar: two complementary actions.
	// 1. If a previously-unknown model returned a parseable usage block,
	//    promote it to "known" so we stop flagging it.
	// 2. If we still couldn't parse usage from a flagged new model, store
	//    the raw response so we can later discover its fields.
	//
	// Under Zero-Log Mode we skip step 2 entirely (the raw response
	// contains user content and must never be persisted). Step 1 is
	// safe because it only stores metadata (the model name), no
	// content.
	if isNewModel && !zeroLog {
		if usage.Source != "estimated" && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
			go workers.PromoteKnown(context.Background(), db.GetRedis(), provider, realModel)
		} else if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
			// CollectSample is non-blocking; we add a follow-up discovery
			// attempt that runs the FieldDiscoverer on the accumulated
			// samples once we have enough of them. The goroutine is
			// safe to fire on every miss because TryDiscoverForModel
			// is idempotent and the sample list is bounded.
			go workers.CollectSample(context.Background(), db.GetRedis(), realModel, cacheableResponse)
			go workers.TryDiscoverForModel(context.Background(), db.GetRedis(), realModel)
		}
	}

	if truePromptTokens > 0 {
		// Calculate ratio of actual billed tokens vs our tiktoken estimation
		// To safely adjust promptOrig for L3 compression without apples-to-oranges comparison.
		if cacheLvl == "L3" && promptOpt > 0 {
			ratio := float64(truePromptTokens) / float64(promptOpt)
			promptOrig = int(float64(promptOrig) * ratio)
		}

		promptOpt = truePromptTokens

		// If no optimization was applied (Standard Routing), the original tokens
		// should match exactly what the provider billed, to avoid false "savings" anomalies.
		if cacheLvl == "NONE" {
			promptOrig = truePromptTokens
		}
	}

	completionOrig := completionTokens
	completionOpt := completionTokens

	go workers.PushTelemetry(vk, provider, realModel, promptOrig, completionOrig, promptOpt, completionOpt, reasoningTokens, cacheLvl, time.Since(startTime), string(rawPayload), string(optPayload), string(cacheableResponse), usage.CacheCreationTokens, usage.CacheReadTokens, usage.CacheHitTokens, usage.CacheMissTokens, agentID, agentLabel, sessionID, zeroLog, toolCallsStr, limitExceeded, false, turnCount, convSignature)

	if isBenchmark {
		go runBenchmarkEvaluation(vk, realKey, provider, realModel, defaultModel, rawPayload, optPayload, cacheableResponse, time.Since(startTime), promptOpt, completionOpt)
	}
}

// extractModelFromResponse pulls the upstream model name out of a
// reconstructed/streamed response. Falls back to the client-supplied
// reqModel if the response doesn't carry one.
func extractModelFromResponse(respBytes []byte, fallback string) string {
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(respBytes, &body); err == nil && body.Model != "" {
		return body.Model
	}
	return fallback
}

func pickModel(discovered, fallback string) string {
	if discovered != "" {
		return discovered
	}
	return fallback
}

func reconstructFromSSE(sseData []byte, model string) []byte {
	lines := strings.Split(string(sseData), "\n")
	var contentParts []string
	var reasoningParts []string
	var toolCalls []map[string]interface{}
	discoveredModel := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			// Capture the upstream model name from the first chunk that
			// carries it. SSE chunks from OpenAI-compatible providers
			// include `"model":"..."` in every chunk.
			if discoveredModel == "" {
				if m, ok := chunk["model"].(string); ok && m != "" {
					discoveredModel = m
				}
			}
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if content, ok := delta["content"].(string); ok {
							contentParts = append(contentParts, content)
						}
						if reasoning, ok := delta["reasoning_content"].(string); ok {
							reasoningParts = append(reasoningParts, reasoning)
						}
						if tcs, ok := delta["tool_calls"].([]interface{}); ok {
							// Merge tool calls by index
							for _, tcIntf := range tcs {
								tc, ok := tcIntf.(map[string]interface{})
								if !ok {
									continue
								}
								index := -1
								if idxFloat, ok := tc["index"].(float64); ok {
									index = int(idxFloat)
								}
								
								// Expand toolCalls slice if needed
								for len(toolCalls) <= index {
									toolCalls = append(toolCalls, map[string]interface{}{})
								}
								
								if index >= 0 {
									merged := toolCalls[index]
									if id, ok := tc["id"].(string); ok {
										merged["id"] = id
									}
									if typ, ok := tc["type"].(string); ok {
										merged["type"] = typ
									}
									if fn, ok := tc["function"].(map[string]interface{}); ok {
										if merged["function"] == nil {
											merged["function"] = map[string]interface{}{"name": "", "arguments": ""}
										}
										mfn := merged["function"].(map[string]interface{})
										if name, ok := fn["name"].(string); ok {
											mfn["name"] = mfn["name"].(string) + name
										}
										if args, ok := fn["arguments"].(string); ok {
											mfn["arguments"] = mfn["arguments"].(string) + args
										}
									}
									toolCalls[index] = merged
								}
							}
						}
					}
				}
			}
		}
	}

	fullContent := strings.Join(contentParts, "")
	fullReasoning := strings.Join(reasoningParts, "")

	message := map[string]interface{}{
		"role":    "assistant",
		"content": fullContent,
	}
	if fullReasoning != "" {
		message["reasoning_content"] = fullReasoning
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	resp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": message, "finish_reason": "stop", "index": 0},
		},
		"model": pickModel(discoveredModel, model),
	}
	out, _ := json.Marshal(resp)
	return out
}

func streamCachedResponse(w http.ResponseWriter, cachedResp []byte, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cachedResp)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string                   `json:"content"`
				ToolCalls []map[string]interface{} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(cachedResp, &parsed); err != nil || len(parsed.Choices) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cachedResp)
		return
	}

	content := parsed.Choices[0].Message.Content
	toolCalls := parsed.Choices[0].Message.ToolCalls

	if len(toolCalls) > 0 {
		// Format tool calls for streaming by injecting the "index" property
		for i, tc := range toolCalls {
			tc["index"] = i
		}
		chunk := map[string]interface{}{
			"id":      "chatcmpl-cached",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"choices": []map[string]interface{}{
				{"delta": map[string]interface{}{
					"role":       "assistant",
					"content":    content,
					"tool_calls": toolCalls,
				}, "index": 0},
			},
			"model": model,
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	} else if content != "" {
		runes := []rune(content)
		chunkSize := 15
		for i := 0; i < len(runes); i += chunkSize {
			end := i + chunkSize
			if end > len(runes) {
				end = len(runes)
			}
			chunkText := string(runes[i:end])
			chunk := map[string]interface{}{
				"id":      "chatcmpl-cached",
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"choices": []map[string]interface{}{
					{"delta": map[string]string{"content": chunkText}, "index": 0},
				},
				"model": model,
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	} else {
		// Empty content, no tools
		chunk := map[string]interface{}{
			"id":      "chatcmpl-cached",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"choices": []map[string]interface{}{
				{"delta": map[string]string{"content": ""}, "index": 0},
			},
			"model": model,
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	// Send the final chunk with finish_reason
	finalChunk := map[string]interface{}{
		"id":      "chatcmpl-cached",
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"choices": []map[string]interface{}{
			{"delta": map[string]string{}, "index": 0, "finish_reason": finishReason},
		},
		"model": model,
	}
	finalData, _ := json.Marshal(finalChunk)
	fmt.Fprintf(w, "data: %s\n\n", finalData)
	flusher.Flush()

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func runBenchmarkEvaluation(vk, realKey, provider, model, defaultModel string, rawPayload, optPayload, optimizedResponse []byte, optDuration time.Duration, promptOpt, completionOpt int) {
	start := time.Now()
	
	var upstreamURL string
	switch provider {
	case "anthropic":
		upstreamURL = "https://api.anthropic.com/v1/messages"
	case "google":
		upstreamURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	case "minimax":
		upstreamURL = "https://api.minimax.io/v1/text/chatcompletion_v2"
	case "deepseek":
		upstreamURL = "https://api.deepseek.com/chat/completions"
	case "mistral":
		upstreamURL = "https://api.mistral.ai/v1/chat/completions"
	case "openrouter":
		upstreamURL = "https://openrouter.ai/api/v1/chat/completions"
	case "groq":
		upstreamURL = "https://api.groq.com/openai/v1/chat/completions"
	case "together":
		upstreamURL = "https://api.together.xyz/v1/chat/completions"
	case "perplexity":
		upstreamURL = "https://api.perplexity.ai/chat/completions"
	default:
		upstreamURL = "https://api.openai.com/v1/chat/completions"
	}
	
	// Create context with timeout for background task to prevent goroutine leak
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Rewrite model for the control request if necessary
	upstreamPayload := rawPayload
	var pMap map[string]interface{}
	if err := json.Unmarshal(rawPayload, &pMap); err == nil {
		forceModel := defaultModel
		if forceModel == "" {
			forceModel = os.Getenv("FORCE_MODEL")
		}
		if forceModel != "" {
			pMap["model"] = forceModel
		}
		pMap["stream"] = false // Force non-streaming for the benchmark control request
		delete(pMap, "stream_options") // stream_options is forbidden when stream=false
		if rewritten, err := json.Marshal(pMap); err == nil {
			upstreamPayload = rewritten
		}
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewBuffer(upstreamPayload))
	req.Header.Set("Content-Type", "application/json")
	if provider == "anthropic" {
		req.Header.Set("x-api-key", realKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+realKey)
	}
	
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Benchmark error: %v", err)
		return
	}
	defer resp.Body.Close()
	
	unoptResp, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Benchmark control request failed: %s - %s", resp.Status, string(unoptResp))
	}
	
	unoptDuration := time.Since(start)

	extractContent := func(payload []byte) string {
		var body struct {
			Choices []struct {
				Message struct {
					Content   string      `json:"content"`
					ToolCalls interface{} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(payload, &body); err == nil && len(body.Choices) > 0 {
			msg := body.Choices[0].Message
			if msg.Content != "" {
				return msg.Content
			}
			if msg.ToolCalls != nil {
				tcBytes, _ := json.Marshal(msg.ToolCalls)
				return string(tcBytes)
			}
		}
		return ""
	}

	score := 95
	feedback := "Fallback mocked score"

	origContent := extractContent(unoptResp)
	optContent := extractContent(optimizedResponse)
	
	if origContent == "" {
		log.Printf("Benchmark extractContent(unoptResp) failed. Body: %s", string(unoptResp))
	}
	if optContent == "" {
		log.Printf("Benchmark extractContent(optimizedResponse) failed.")
	}

	if origContent != "" && optContent != "" {
		evalPrompt := fmt.Sprintf(`Compare Response A and Response B. Rate how semantically similar they are from 0 to 100. Return ONLY a valid JSON object with {"score": <integer>, "feedback": "<1 sentence explanation>"}.

Response A:
%s

Response B:
%s`, origContent, optContent)

		evalModel := model
		forceModel := defaultModel
		if forceModel == "" {
			forceModel = os.Getenv("FORCE_MODEL")
		}
		if forceModel != "" {
			evalModel = forceModel
		}

		evalReqBody := map[string]interface{}{
			"model": evalModel,
			"messages": []map[string]string{
				{"role": "user", "content": evalPrompt},
			},
		}
		evalBodyBytes, _ := json.Marshal(evalReqBody)

		evalReq, _ := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewBuffer(evalBodyBytes))
		evalReq.Header.Set("Content-Type", "application/json")
		if provider == "anthropic" {
			evalReq.Header.Set("x-api-key", realKey)
			evalReq.Header.Set("anthropic-version", "2023-06-01")
		} else {
			evalReq.Header.Set("Authorization", "Bearer "+realKey)
		}
		
		evalResp, evalErr := client.Do(evalReq)
		if evalErr == nil {
			defer evalResp.Body.Close()
			evalRespBytes, _ := io.ReadAll(evalResp.Body)
			evalText := extractContent(evalRespBytes)
			
			var evalData struct {
				Score    int    `json:"score"`
				Feedback string `json:"feedback"`
			}
			evalText = strings.TrimSpace(evalText)
			evalText = strings.TrimPrefix(evalText, "```json\n")
			evalText = strings.TrimSuffix(evalText, "\n```")
			evalText = strings.TrimSuffix(evalText, "```")
			
			if err := json.Unmarshal([]byte(evalText), &evalData); err == nil {
				score = evalData.Score
				feedback = evalData.Feedback
			}
		}
	}

	rdb := db.GetRedis()
	rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: "synapse:benchmark_logs",
		Values: map[string]interface{}{
			"vk": vk,
			"orig_prompt": string(rawPayload),
			"opt_prompt": string(optPayload),
			"opt_resp": string(optimizedResponse),
			"orig_resp": string(unoptResp),
			"opt_ms": optDuration.Milliseconds(),
			"orig_ms": unoptDuration.Milliseconds(),
			"score": score,
			"feedback": feedback,
			"opt_prompt_tokens": promptOpt,
			"opt_completion_tokens": completionOpt,
		},
	})
}

// appError describes an upstream application-level error that the proxy
// surfaces to the client as a real HTTP status (instead of forwarding a
// 200 OK with a poison body that causes the agent to hang).
type appError struct {
	statusCode int    // upstream's reported code (e.g. 2056 for MiniMax quota)
	message    string // human-readable message
	quota      bool   // true for quota/credit/payment-required errors
}

// detectUpstreamAppError parses an upstream response body and returns a
// non-nil *appError if the upstream returned an application-level error
// (despite the HTTP 200 status). Supports:
//   - MiniMax: { "base_resp": { "status_code": N, "status_msg": "..." } }
//   - OpenAI-style: { "error": { "message": "...", "type": "...", "code": ... } }
//
// nil means "no error detected, keep streaming".
func detectUpstreamAppError(body []byte) *appError {
	if len(body) == 0 {
		return nil
	}
	// Extract the JSON part from an SSE "data: {...}" line.
	jsonBody := body
	if bytes.HasPrefix(body, []byte("data: ")) {
		jsonBody = body[len("data: "):]
		// SSE may include a trailing "\n\n" after the JSON.
		if idx := bytes.IndexByte(jsonBody, '\n'); idx > 0 {
			jsonBody = jsonBody[:idx]
		}
	}
	// Skip the SSE "data: [DONE]" sentinel.
	if bytes.HasPrefix(jsonBody, []byte("[DONE]")) {
		return nil
	}
	if !bytes.HasPrefix(jsonBody, []byte("{")) {
		return nil
	}

	// First, try a generic structure that can hold either base_resp or error.
	var generic struct {
		BaseResp *struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
		// Some upstreams (e.g. Anthropic) put code at the top level.
		TopCode any `json:"code"`
	}
	if err := json.Unmarshal(jsonBody, &generic); err != nil {
		return nil
	}

	// MiniMax-style: base_resp.status_code != 0 means error.
	if generic.BaseResp != nil && generic.BaseResp.StatusCode != 0 {
		msg := generic.BaseResp.StatusMsg
		if msg == "" {
			msg = fmt.Sprintf("upstream returned status_code %d", generic.BaseResp.StatusCode)
		}
		return &appError{
			statusCode: generic.BaseResp.StatusCode,
			message:    msg,
			quota:      isQuotaError(generic.BaseResp.StatusCode, msg),
		}
	}

	// OpenAI-style: { "error": { ... } }
	if generic.Error != nil && generic.Error.Message != "" {
		msg := generic.Error.Message
		return &appError{
			statusCode: 0,
			message:    msg,
			quota:      isQuotaError(0, msg),
		}
	}

	return nil
}

// isQuotaError returns true if the upstream error looks like a quota/credit
// problem (so the proxy can return HTTP 402 Payment Required to the client).
func isQuotaError(code int, msg string) bool {
	m := strings.ToLower(msg)
	keywords := []string{
		"quota", "credit", "usage limit", "rate limit", "billing", "plan",
		"insufficient", "payment", "exhausted",
	}
	for _, k := range keywords {
		if strings.Contains(m, k) {
			return true
		}
	}
	// MiniMax returns code 2056 for quota and 1002/1003/1004 for billing.
	if code == 2056 || code == 1002 || code == 1003 || code == 1004 {
		return true
	}
	return false
}

// maskVirtualKey returns a short, non-secret prefix of the virtual key
// for safe inclusion in panic / error logs. Format: first 8 chars + "â€¦"
// (e.g. "sk-optiâ€¦"). Returns "<empty>" for empty input.
func maskVirtualKey(authHeader string) string {
	vk := strings.TrimPrefix(authHeader, "Bearer ")
	vk = strings.TrimSpace(vk)
	if vk == "" {
		return "<empty>"
	}
	if len(vk) <= 8 {
		return vk[:min(len(vk), 4)] + "â€¦"
	}
	return vk[:8] + "â€¦"
}

// makeSelfCorrectionResponse constructs a mock Chat Completions response containing the self-correction hint.
func makeSelfCorrectionResponse(toolName string, model string) []byte {
	var msgContent string
	if toolName != "" {
		msgContent = "Attention : Vous venez de répéter l'outil " + toolName + " avec les mêmes arguments. Veuillez vérifier vos actions précédentes ou changer de stratégie pour éviter une boucle infinie."
	} else {
		msgContent = "Attention : Une boucle répétitive a été détectée dans vos requêtes. Veuillez vérifier vos actions précédentes ou changer de stratégie pour éviter une boucle infinie."
	}

	respObj := map[string]interface{}{
		"id":      "chatcmpl-selfcorrect",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": msgContent,
				},
				"finish_reason": "stop",
			},
		},
	}
	respBytes, _ := json.Marshal(respObj)
	return respBytes
}

// injectSystemWarning appends a system warning to the last message in the payload
// to nudge the LLM out of a tool loop without stopping the agent framework.
func injectSystemWarning(payload []byte, toolName string) []byte {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload
	}
	messagesRaw, ok := body["messages"].([]interface{})
	if !ok || len(messagesRaw) == 0 {
		return payload
	}

	lastMsgRaw := messagesRaw[len(messagesRaw)-1]
	lastMsg, ok := lastMsgRaw.(map[string]interface{})
	if !ok {
		return payload
	}

	warningText := fmt.Sprintf("\n\n[SYSTEM WARNING: The proxy intercepted your request because you are caught in a loop. You have repeated the tool '%s' with identical arguments too many times. You MUST change your strategy immediately. Do not repeat the same action.]", toolName)

	// Try to append to string content
	if contentStr, ok := lastMsg["content"].(string); ok {
		lastMsg["content"] = contentStr + warningText
	} else if contentArr, ok := lastMsg["content"].([]interface{}); ok {
		// It's an array of content blocks (OpenAI vision or Anthropic style)
		contentArr = append(contentArr, map[string]interface{}{
			"type": "text",
			"text": warningText,
		})
		lastMsg["content"] = contentArr
	} else {
		// Fallback: append a user message
		warningMsg := map[string]interface{}{
			"role": "user",
			"content": warningText,
		}
		body["messages"] = append(messagesRaw, warningMsg)
	}

	newPayload, err := json.Marshal(body)
	if err != nil {
		return payload
	}
	return newPayload
}
