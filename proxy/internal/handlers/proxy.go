package handlers

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"synapse-proxy/cache"
	"synapse-proxy/internal/db"
	"synapse-proxy/internal/metrics"
	"synapse-proxy/internal/services"
	"synapse-proxy/internal/utils"
	"synapse-proxy/internal/workers"
	"synapse-proxy/optiagent"
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
		if isolateCache {
			if userStr, ok := payloadMap["user"].(string); ok && userStr != "" {
				virtualKey = virtualKey + ":" + userStr
			}
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
	// debug removed

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
	//
	// This block was migrated to optiagent.ToolFilterHook (which
	// runs the denylist + allowlist in one go and surfaces a
	// short-circuit on either path). The hook also handles the
	// tool dedup observation. We still keep the inline
	// fileToolCalls extract here because the legacy cache engine
	// reads it downstream.
	hctx := &optiagent.HookContext{
		VK:               virtualKey,
		Provider:         provider,
		Model:            reqModel,
		SessionID:        sessionID,
		TurnCount:        turnCount,
		ConvSignature:    convSignature,
		RawPayload:       bodyBytes,
		OptimizedPayload: bodyBytes, // L3 hasn't run yet; same as raw
		ResponseHeaders:  w.Header(),
		Features: map[string]interface{}{
			"kill_switch":             keyConfig.KillSwitch,
			"fingerprint_loop_detect": keyConfig.FingerprintLoopDetect,
			"session_token_limit":     keyConfig.SessionTokenLimit,
			"block_unknown_tools":     keyConfig.BlockUnknownTools,
			"allowed_tools":           keyConfig.AllowedTools,
			"default_model":           keyConfig.DefaultModel,
		},
	}
	optiagent.SetFingerprintEnabled(virtualKey, keyConfig.FingerprintLoopDetect)
	if _, shortCircuited := optiagent.RunBeforeHooks(ctx, hctx); shortCircuited {
		if hctx.ShortCircuitStatus != 0 && len(hctx.ShortCircuitBody) > 0 {
			w.Header().Set("Content-Type", "application/json")
			if _, isCCR := hctx.Feature("ccr_cache_hit"); isCCR {
				w.Header().Set("X-SynapseProxy-Cache", "L3")
				cachedUsage := utils.ExtractUsage(hctx.ShortCircuitBody)
				promptOrig := utils.CountTokens(string(bodyBytes))
				promptOpt := utils.CountTokens(string(hctx.OptimizedPayload))
				go workers.PushTelemetry(virtualKey, provider, reqModel,
					promptOrig, cachedUsage.CompletionTokens, promptOpt, 0, cachedUsage.ReasoningTokens,
					"L3", time.Since(startTime), string(bodyBytes), string(hctx.OptimizedPayload), string(hctx.ShortCircuitBody),
					cachedUsage.CacheCreationTokens, cachedUsage.CacheReadTokens, cachedUsage.CacheHitTokens, cachedUsage.CacheMissTokens,
					"", "", sessionID, zeroLog, toolCallsStr, keyConfig.LimitExceeded, false,
					turnCount, convSignature, workers.BuildPerHookSavingsJSON(hctx))
			}
			w.WriteHeader(hctx.ShortCircuitStatus)
			w.Write(hctx.ShortCircuitBody)
			return
		}
		log.Printf("[ProxyHandler] hook pipeline short-circuit (status=%d, body=%d bytes)",
			hctx.ShortCircuitStatus, len(hctx.ShortCircuitBody))
	}
	// Apply any payload mutation the hooks made (currently none of
	// the migrated hooks mutate the request body, but keeping the
	// mechanism here means future hooks can do so without further
	// wiring).
	if hctx.FinalOptimizedPayload != nil && len(hctx.FinalOptimizedPayload) > 0 {
		bodyBytes = hctx.FinalOptimizedPayload
	}


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
				turnCount, convSignature, workers.BuildPerHookSavingsJSON(hctx))
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
			"L0", time.Since(startTime), string(bodyBytes), string(bodyBytes), string(resp), 0, 0, 0, 0, agentSig.ID, agentSig.Label, sessionID, zeroLog, toolCallsStr, keyConfig.LimitExceeded, false, turnCount, convSignature, workers.BuildPerHookSavingsJSON(hctx))
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

		// BUG FIX: was `var optResult` which created a NEW local variable
		// that shadowed the function-level optResult (line 153). The closure
		// in executeRequest captured the outer (empty) variable, causing
		// upstream payload len=0. Now assigns to the existing function-level var.
		//
		// After RunBeforeHooks, hctx.OptimizedPayload holds the post-hook
		// payload (potentially compressed by the byte-preserving L3
		// hook). We use THAT, not the raw bodyBytes, so that the L3
		// savings actually reach the upstream provider. The
		// PromptTokensOpt counter is computed on the post-hook
		// payload so the dashboard's "In saved" / "out saved" reflect
		// the actual savings.
		optimizedPayload := hctx.OptimizedPayload
		if len(optimizedPayload) == 0 {
			optimizedPayload = bodyBytes
		}
		optResult = optiagent.OptimizationResult{
			Payload:          optimizedPayload,
			PayloadHash:      optiagent.HashPayload(optimizedPayload),
			PromptTokensOrig: utils.CountTokens(string(bodyBytes)),
			PromptTokensOpt:  utils.CountTokens(string(optimizedPayload)),
		}
		if saved := len(bodyBytes) - len(optimizedPayload); saved > 0 {
			log.Printf("[strangler] hook-only mode with L3 compression: raw=%d -> compressed=%d bytes (saved %d = %.1f%%) hash=%s",
				len(bodyBytes), len(optimizedPayload), saved,
				100.0*float64(saved)/float64(len(bodyBytes)),
				optResult.PayloadHash[:12])
		} else {
			log.Printf("[strangler] hook-only mode: payload=%d bytes hash=%s", len(optResult.Payload), optResult.PayloadHash[:12])
		}

		// 1. L1 Cache (Exact Match)
		if keyConfig.EnableL1 {
			l1Key := "synapse:l1cache:" + virtualKey + ":" + optResult.PayloadHash
			cachedResp, err := rdb.Get(ctx, l1Key).Bytes()
			if err == nil && len(cachedResp) > 0 {
				if optiagent.ShouldReuseCache(ctx, rdb, bodyBytes, l1Key, cacheTtl, keyConfig.ToolTtls) {
					if !keyConfig.LimitExceeded {
						optResult.CacheHitLevel = "L1"
						optResult.HitResponse = cachedResp
					}
				}
			}
		}

		// 2. L2 Cache (Semantic Match)
		if (optResult.CacheHitLevel == "NONE" || optResult.CacheHitLevel == "") && keyConfig.EnableL2 && cache.GlobalEmbedder != nil && !forceDisableL2 {
			embeddingText, hasImage, _ := optiagent.ExtractTextForEmbedding(bodyBytes)
			if !hasImage {
				vector, err := cache.GlobalEmbedder.GenerateEmbedding(embeddingText)
				if err == nil && len(vector) > 0 {
					optResult.Vector = vector
					buf := new(bytes.Buffer)
					if binary.Write(buf, binary.LittleEndian, vector) == nil {
						escapedVK := optiagent.EscapeRedisTag(virtualKey)
						query := "(@vk:{" + escapedVK + "})=>[KNN 1 @vector $query_vec AS score]"
						res, err := rdb.Do(ctx, "FT.SEARCH", "idx:l2cache", query, "PARAMS", "2", "query_vec", buf.Bytes(), "RETURN", "2", "score", "response", "DIALECT", "2").Result()
						if err == nil {
							resArr, ok := res.([]interface{})
							if ok && len(resArr) > 2 {
								fields, ok := resArr[2].([]interface{})
								if ok {
									var score float64
									var hitResponse string
									for i := 0; i < len(fields); i += 2 {
										key := fields[i].(string)
										if key == "score" {
											score, _ = strconv.ParseFloat(fields[i+1].(string), 64)
										} else if key == "response" {
											hitResponse = fields[i+1].(string)
										}
									}
									if score < semanticTolerance && hitResponse != "" {
										docKey, _ := resArr[1].(string)
										if optiagent.ShouldReuseCache(ctx, rdb, bodyBytes, docKey, cacheTtl, keyConfig.ToolTtls) {
											if !keyConfig.LimitExceeded {
												optResult.CacheHitLevel = "L2"
												optResult.HitResponse = []byte(hitResponse)
											}
										}
									}
								}
							}
						}
					}
				}
			}
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
				turnCount, convSignature, workers.BuildPerHookSavingsJSON(hctx))

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
				turnCount, convSignature, workers.BuildPerHookSavingsJSON(hctx))
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
			turnCount, convSignature, workers.BuildPerHookSavingsJSON(hctx))
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

			// TEMPORARY: disable injectSystemWarning for debugging
			// optResult.Payload = injectSystemWarning(optResult.Payload, fpTool)
			// optResult.PromptTokensOpt += 40

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
			targetURL = "https://api.minimax.io/v1/chat/completions"
			if envURL := os.Getenv("MINIMAX_UPSTREAM_URL"); envURL != "" {
				targetURL = envURL
			}
		case "minimax-anthropic":
			// Anthropic-native upstream path for Minimax. The
			// payload has already been converted to the
			// Anthropic /v1/messages shape by the
			// AnthropicEndpointHook (priority 800).
			targetURL = "https://api.minimax.io/anthropic/v1/messages"
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
		case "lmstudio":
			targetURL = "http://127.0.0.1:1234/v1/chat/completions"
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

		// NO TRANSLATION: minimax handles /v1/chat/completions
		// in OpenAI format directly. The earlier translator code
		// (OpenAIToAnthropic) was based on the assumption that
		// minimax exposed /v1/messages — it does not. Forwarding
		// the OpenAI-shape body as-is works, translating it
		// produces a 400 'missing required parameter: model'
		// from the /v1/chat/completions endpoint.
		//
		// The Anthropic-format translation code in
		// proxy/optiagent/openai_to_anthropic.go is kept for
		// forward compatibility (e.g. if we add a real Anthropic
		// provider in the future) but is NOT invoked from this
		// hot path.

		req, err := http.NewRequest("POST", targetURL, bytes.NewBuffer(upstreamPayload))
		if err != nil {
			return nil, err
		}

		req.Header.Set("Content-Type", "application/json")
		if currentProvider == "anthropic" || currentProvider == "minimax-anthropic" {
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
	// Switch to the Anthropic-native upstream when the VK has
	// opted in. Routing /anthropic/v1/messages (rather than
	// /v1/chat/completions) is what unlocks Minimax's prompt
	// cache at 0.1x input rate per the vendor docs.
	if keyConfig != nil && keyConfig.UseAnthropicEndpoint && provider == "minimax" {
		usedProvider = "minimax-anthropic"
	}

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

		// Telemetry: log the L3 savings even when the upstream
		// returned a 4xx/5xx. Without this, the L3 byte-preserving
		// savings are invisible in the dashboard for any request
		// that the upstream rejects (e.g. 400 from a payload
		// mismatch after a tool_call_id reshuffle). The hook has
		// already compressed the payload; we just need to record
		// the savings so the operator can see the compression
		// actually ran.
		go workers.PushTelemetry(virtualKey, provider, reqModel,
			optResult.PromptTokensOrig, 0, optResult.PromptTokensOpt, 0, 0,
			"L3", time.Since(startTime), string(bodyBytes), string(optResult.Payload), errBody,
			0, 0, 0, 0, agentSig.ID, agentSig.Label, sessionID, zeroLog,
			toolCallsStr, keyConfig.LimitExceeded, true,
			turnCount, convSignature, workers.BuildPerHookSavingsJSON(hctx))

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
	} else if resp != nil && finalResponseBytes != nil {
		// Normal path: resp.Body was consumed by io.ReadAll in the
		// tool-call interception loop. Re-wrap the bytes we saved.
		resp.Body = io.NopCloser(bytes.NewBuffer(finalResponseBytes))
	} else if resp != nil {
		// Edge case: the for-loop exited without reading the body
		// (e.g. non-200 status). resp.Body is still the original
		// stream — leave it untouched for streamResponse to read.
	}
	if resp == nil {
		log.Printf("[ProxyHandler] resp is nil after upstream — cannot stream response")
		http.Error(w, `{"error":"upstream returned no response"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	streamResponse(w, resp, virtualKey, realKey, usedProvider, reqModel, defaultModel, optResult.PayloadHash, reqModel, optResult.Vector, optResult.PromptTokensOrig, optResult.PromptTokensOpt, optResult.CacheHitLevel, isBenchmark, bodyBytes, optResult.Payload, startTime, wantStream, cacheTtl, isNewModel, agentSig.ID, agentSig.Label, sessionID, zeroLog, &l0PublishResponse, toolCallsStr, keyConfig.LimitExceeded, turnCount, convSignature, hctx)
}
