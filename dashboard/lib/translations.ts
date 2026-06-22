import { prisma } from "@/lib/prisma";

export type Language = "fr" | "en";

export interface TranslationItem {
  metaTitle: string;
  metaDesc: string;
  heroBadge: string;
  heroTitle: string;
  heroDesc: string;
  backBtn: string;
  dashboardBtn: string;
  // Sections
  sections: {
    title: string;
    desc: string;
    items: string[];
    color: string;
    price?: string;
    cta?: string;
    href?: string;
    highlight?: boolean;
    mediaUrl?: string;
    mediaSize?: "small" | "medium" | "large" | "full" | string;
  }[];
  // Video Section
  videoTitle: string;
  videoDesc: string;
  videoAlt: string;
  videoUrl?: string;
  videoConsoleTitle?: string;
  videoConsoleItems?: string[];
  // Comparison table (optional)
  table?: {
    headers: string[];
    rows: {
      feature: string;
      synapse: string;
      other: string;
    }[];
  };
}

export const translations: Record<string, Record<Language, TranslationItem>> = {
  pricing: {
    fr: {
      metaTitle: "Tarifs — Synapse Proxy",
      metaDesc: "Choisissez le plan adapté à votre utilisation de Synapse Proxy.",
      heroBadge: "Tarifs",
      heroTitle: "Un pricing transparent, comme notre proxy.",
      heroDesc: "Commencez gratuitement, payez uniquement quand vos agents génèrent de la valeur réelle.",
      backBtn: "Retour",
      dashboardBtn: "Tableau de Bord",
      videoTitle: "", videoDesc: "", videoAlt: "",
      sections: [
        {
          title: "Developer",
          price: "0",
          desc: "Parfait pour expérimenter avec les agents autonomes en local.",
          items: ["Jusqu'à 100 000 requêtes / mois", "Cache L1 (Exact Match)", "Pare-feu Agentique (Limites basiques)", "Serveur MCP inclus", "Support communautaire"],
          color: "emerald", cta: "Commencer gratuitement", href: "/signup", highlight: false
        },
        {
          title: "Startup",
          price: "49",
          desc: "Pour les équipes en production qui ont besoin d'observabilité.",
          items: ["Jusqu'à 1M requêtes / mois", "Cache L2 Sémantique (ONNX)", "Pare-feu Agentique complet (Kill Switch)", "Classification d'Intents locale (0ms latency)", "Rétention des logs 30 jours", "Support prioritaire"],
          color: "teal", cta: "Démarrer l'essai", href: "/signup", highlight: true
        },
        {
          title: "Enterprise",
          price: "Sur mesure",
          desc: "Déploiement souverain, SLA et conformité totale.",
          items: ["Requêtes illimitées", "Déploiement VPC / On-Premise", "Cache L3 (Compression de Contexte)", "SSO & RBAC (Multi-tenant)", "Redaction PII automatique", "Ingénieur support dédié"],
          color: "blue", cta: "Contacter les ventes", href: "mailto:contact@synapse-proxy.com", highlight: false
        }
      ]
    },
    en: {
      metaTitle: "Pricing — Synapse Proxy",
      metaDesc: "Choose the right plan for your Synapse Proxy usage.",
      heroBadge: "Pricing",
      heroTitle: "Transparent pricing, just like our proxy.",
      heroDesc: "Start for free, pay only when your agents generate real value.",
      backBtn: "Back",
      dashboardBtn: "Dashboard",
      videoTitle: "", videoDesc: "", videoAlt: "",
      sections: [
        {
          title: "Developer",
          price: "0",
          desc: "Perfect for experimenting with autonomous agents locally.",
          items: ["Up to 100k requests / month", "L1 Cache (Exact Match)", "Agentic Firewall (Basic limits)", "MCP Server included", "Community support"],
          color: "emerald", cta: "Start for free", href: "/signup", highlight: false
        },
        {
          title: "Startup",
          price: "49",
          desc: "For production teams that need deep observability.",
          items: ["Up to 1M requests / month", "L2 Semantic Cache (ONNX)", "Full Agentic Firewall (Kill Switch)", "Local Intent Classification (0ms latency)", "30-day log retention", "Priority support"],
          color: "teal", cta: "Start free trial", href: "/signup", highlight: true
        },
        {
          title: "Enterprise",
          price: "Custom",
          desc: "Sovereign deployment, SLA, and full compliance.",
          items: ["Unlimited requests", "VPC / On-Premise deployment", "L3 Cache (Context Compression)", "SSO & RBAC (Multi-tenant)", "Automatic PII Redaction", "Dedicated support engineer"],
          color: "blue", cta: "Contact sales", href: "mailto:contact@synapse-proxy.com", highlight: false
        }
      ]
    }
  },
  caching: {
    fr: {
      metaTitle: "Cache Sémantique LLM & Optimisation Coûts API | Synapse Proxy",
      metaDesc: "Réduisez vos coûts d'API OpenAI/Anthropic jusqu'à 80% avec le cache triple niveau L1 (Exact), L2 (Sémantique locale ONNX) et L3 (Compression de contexte) de Synapse Proxy.",
      heroBadge: "Cache",
      heroTitle: "Cache LLM Multi-Niveaux",
      heroDesc: "Économisez jusqu'à 80% sur vos appels d'API d'intelligence artificielle en interceptant les requêtes redondantes au niveau réseau.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "Cache Exact (Fast Hash) - L1",
          desc: "Compare instantanément l'empreinte cryptographique des invites. Si la structure et le contenu sont identiques, le résultat est servi en moins de 5ms à coût zéro.",
          items: ["Latence inférieure à 5ms", "Économie financière de 100%", "Pas d'appel LLM de validation"],
          color: "emerald"
        },
        {
          title: "Cache Sémantique Local - L2",
          desc: "Utilise un modèle d'embeddings sémantiques local (ONNX MiniLM) et une recherche vectorielle Redis VSS pour intercepter les requêtes sémantiquement équivalentes.",
          items: ["Recherche vectorielle locale ultra-rapide", "Tolérance sémantique ajustable", "Idéal pour les questions récurrentes"],
          color: "teal"
        },
        {
          title: "Compression de Contexte - L3",
          desc: "Compresse les historiques de discussion volumineux en éliminant les tokens redondants ou à faible valeur d'information, réduisant ainsi la taille du prompt de 30 à 50%.",
          items: ["Pruning de contexte intelligent", "Préservation du sens global", "Diminution importante de la latence"],
          color: "cyan"
        }
      ],
      videoTitle: "Télémétrie de Cache en Temps Réel",
      videoDesc: "Visualisez les économies instantanées générées par votre infrastructure de cache. Notre tableau de bord affiche en direct le ratio de hits L1/L2/L3, les volumes de tokens optimisés ainsi que la conversion directe en dollars préservés.",
      videoAlt: "Démonstration Télémétrie Caching",
      videoConsoleTitle: "Playground Cache Status",
      videoConsoleItems: [
        "X-SynapseProxy-Cache: L1 (Hit)",
        "X-SynapseProxy-Latency: 2ms",
        "Upstream Billing: $0.000000"
      ]
    },
    en: {
      metaTitle: "Semantic LLM Caching & API Cost Optimization | Synapse Proxy",
      metaDesc: "Reduce OpenAI/Anthropic API costs by up to 80% with Synapse Proxy's L1 (Exact), L2 (Local ONNX Semantic), and L3 (Context Compression) triple-level cache.",
      heroBadge: "Cache",
      heroTitle: "Multi-Level LLM Caching",
      heroDesc: "Save up to 80% on your AI API calls by intercepting redundant requests directly at the network level.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "Exact Cache (Fast Hash) - L1",
          desc: "Instantly compares the cryptographic footprint of prompts. If the structure and content are identical, the result is served in under 5ms at zero cost.",
          items: ["Latency under 5ms", "100% financial savings", "No upstream LLM validation call"],
          color: "emerald"
        },
        {
          title: "Local Semantic Cache - L2",
          desc: "Uses a local semantic embedding model (ONNX MiniLM) and Redis VSS vector search to intercept semantically equivalent requests.",
          items: ["Ultra-fast local vector search", "Adjustable semantic tolerance", "Ideal for recurring user queries"],
          color: "teal"
        },
        {
          title: "Context Compression - L3",
          desc: "Compresses large chat histories by eliminating redundant or low-information tokens, reducing prompt size by 30% to 50%.",
          items: ["Intelligent context pruning", "Overall meaning preservation", "Significant reduction in latency"],
          color: "cyan"
        }
      ],
      videoTitle: "Real-Time Cache Telemetry",
      videoDesc: "Visualize the instant savings generated by your caching infrastructure. Our dashboard displays the L1/L2/L3 hit ratio, optimized token volumes, and direct dollar savings in real time.",
      videoAlt: "Caching Telemetry Demo",
      videoConsoleTitle: "Playground Cache Status",
      videoConsoleItems: [
        "X-SynapseProxy-Cache: L1 (Hit)",
        "X-SynapseProxy-Latency: 2ms",
        "Upstream Billing: $0.000000"
      ]
    }
  },
  firewall: {
    fr: {
      metaTitle: "Pare-feu Agentique & Protection Anti-Boucle LLM | Synapse Proxy",
      metaDesc: "Sécurisez vos agents IA autonomes contre les boucles d'appels d'outils infinies et les injections de prompts malveillantes grâce à notre Agentic Firewall.",
      heroBadge: "Firewall",
      heroTitle: "Agentic Firewall",
      heroDesc: "Stoppez les boucles de requêtes incontrôlables des agents autonomes et sécurisez l'accès aux API d'IA en temps réel.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "Protection Anti-Boucle (Loop Detect)",
          desc: "Les agents d'IA peuvent parfois entrer dans des boucles de répétition d'outils infinies (ex: appeler sans cesse le même outil en cas d'erreur de parsing). Synapse Proxy détecte ces empreintes comportementales identiques et coupe la boucle.",
          items: ["Détection d'empreinte de paramètres d'outils", "Injection d'instruction d'auto-correction", "Économie de milliers de tokens perdus"],
          color: "red"
        },
        {
          title: "Blocage d'Outils Non Autorisés",
          desc: "Restreignez les outils (tools/functions) que vos LLMs peuvent appeler en production pour chaque clé virtuelle. Évite les injections indirectes de commandes malveillantes via l'agent.",
          items: ["Allowlist stricte de fonctions", "Masquage automatique des PII (données sensibles)", "Limitation de jetons par session d'exécution"],
          color: "amber"
        }
      ],
      videoTitle: "Détection et Auto-correction de Boucle en Direct",
      videoDesc: "Regardez comment Synapse Proxy intercepte en direct un agent autonome pris au piège dans une boucle de répétition. Le pare-feu intercepte la 3ème tentative identique, injecte une instruction d'auto-correction, et débloque l'agent.",
      videoAlt: "Démonstration Loop Detection Agentic Firewall",
      videoConsoleTitle: "Firewall Activity Logs",
      videoConsoleItems: [
        "[WARNING] Loop detected on tool 'read_file'",
        "[INJECTED] \"System: You have called read_file with the same arguments 3 times. Try using listing first.\"",
        "[RESOLVED] Agent recovered in next turn."
      ]
    },
    en: {
      metaTitle: "Agentic Firewall & LLM Loop Protection | Synapse Proxy",
      metaDesc: "Secure your autonomous AI agents against infinite tool-call loops and prompt injections with our Agentic Firewall.",
      heroBadge: "Firewall",
      heroTitle: "Agentic Firewall",
      heroDesc: "Stop uncontrollable query loops from autonomous agents and secure AI API access in real time.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "Anti-Loop Protection (Loop Detect)",
          desc: "AI agents can sometimes enter infinite tool repetition loops (e.g., repeatedly calling the same tool on parsing errors). Synapse Proxy detects these behavioral footprints and breaks the loop.",
          items: ["Tool parameter footprint detection", "Self-correction instruction injection", "Savings of thousands of wasted tokens"],
          color: "red"
        },
        {
          title: "Unauthorized Tool Blocking",
          desc: "Restrict which tools (functions) your LLMs can call in production per virtual key. Prevents indirect command injection via the agent.",
          items: ["Strict function allowlisting", "Automatic PII masking (sensitive data)", "Token limit per execution session"],
          color: "amber"
        }
      ],
      videoTitle: "Live Loop Detection & Self-Correction",
      videoDesc: "Watch Synapse Proxy intercept an autonomous agent stuck in a repetition loop in real time. The firewall stops the 3rd identical attempt, injects a system correction prompt, and recovers the agent.",
      videoAlt: "Agentic Firewall Loop Detection Demo",
      videoConsoleTitle: "Firewall Activity Logs",
      videoConsoleItems: [
        "[WARNING] Loop detected on tool 'read_file'",
        "[INJECTED] \"System: You have called read_file with the same arguments 3 times. Try using listing first.\"",
        "[RESOLVED] Agent recovered in next turn."
      ]
    }
  },
  compression: {
    fr: {
      metaTitle: "Compression de Contexte LLM & Pruning de Tokens | Synapse Proxy",
      metaDesc: "Diminuez le coût d'entrée de vos prompts LLM de 30% à 50% grâce au pruning de contexte intelligent intégré à la passerelle de Synapse Proxy.",
      heroBadge: "Compression",
      heroTitle: "Compression de Contexte",
      heroDesc: "Optimisez vos fenêtres de contexte LLM en supprimant les tokens non informatifs avant l'envoi de la requête au fournisseur d'API.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "Pruning de Tokens Intelligent (L3)",
          desc: "Notre passerelle réseau intègre un parseur qui analyse l'importance d'attention de chaque token du prompt. Les préfixes longs ou l'historique de discussion sont élagués de manière à préserver le sens logique global.",
          items: ["Compression de 30% à 50% sur l'historique", "Réduction de latence sur le premier token (TTFT)", "Préservation totale de la cohérence sémantique"],
          color: "cyan"
        },
        {
          title: "Intégration Transparente",
          desc: "Contrairement aux librairies manuelles comme LLMLingua, Synapse Proxy compresse les requêtes de manière transparente au niveau réseau : aucune modification de code requise pour vos applications clientes.",
          items: ["Zéro modification du code applicatif", "Compatible avec tous les SDK standard (OpenAI, Anthropic)", "Performance optimisée pour les requêtes à forte latence"],
          color: "purple"
        }
      ],
      videoTitle: "Analyse sémantique de la réduction de jetons",
      videoDesc: "Regardez comment Synapse Proxy suit et enregistre la compression des requêtes d'historique. L'onglet analytique affiche le taux d'économie par type de jetons, les performances de compression en millisecondes et met en évidence la réduction de la facture.",
      videoAlt: "Vidéo Token Compression",
      videoConsoleTitle: "Compression Metrics",
      videoConsoleItems: [
        "Origine Prompt: 12,450 tokens",
        "Compressé Prompt: 7,210 tokens (42% de réduction)",
        "Économie de Latence: -1.2 seconde sur le premier token"
      ]
    },
    en: {
      metaTitle: "LLM Context Compression & Token Pruning | Synapse Proxy",
      metaDesc: "Reduce your LLM prompt costs by 30% to 50% using intelligent context pruning built into the Synapse Proxy gateway.",
      heroBadge: "Compression",
      heroTitle: "Context Compression",
      heroDesc: "Optimize your LLM context windows by stripping non-informative tokens before sending the query to the API provider.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "Intelligent Token Pruning (L3)",
          desc: "Our network gateway features a parser that analyzes the attention weight of each token. Long prefixes or chat history are pruned to preserve global semantic meaning.",
          items: ["30% to 50% compression on chat history", "Reduced Time to First Token (TTFT)", "Complete preservation of semantic coherence"],
          color: "cyan"
        },
        {
          title: "Transparent Integration",
          desc: "Unlike manual libraries like LLMLingua, Synapse Proxy compresses queries transparently at the network layer: no code changes required for your clients.",
          items: ["Zero client code modifications", "Compatible with all standard SDKs (OpenAI, Anthropic)", "Performance optimized for high-latency queries"],
          color: "purple"
        }
      ],
      videoTitle: "Semantic Token Reduction Analytics",
      videoDesc: "Watch how Synapse Proxy tracks and logs history request compression. The analytics tab shows savings rate by token type, compression latency in milliseconds, and total bill reduction.",
      videoAlt: "Token Compression Video",
      videoConsoleTitle: "Compression Metrics",
      videoConsoleItems: [
        "Original Prompt: 12,450 tokens",
        "Compressed Prompt: 7,210 tokens (42% reduction)",
        "Latency Savings: -1.2 seconds on first token"
      ]
    }
  },
  mcp: {
    fr: {
      metaTitle: "Serveur MCP pour Cursor & Claude Desktop | Synapse Proxy",
      metaDesc: "Pilotez et observez votre passerelle d'IA directement depuis vos éditeurs de code et invites d'agents autonomes via le protocole MCP.",
      heroBadge: "MCP",
      heroTitle: "Serveur MCP Intégré",
      heroDesc: "Exploitez la puissance du Model Context Protocol (MCP) pour connecter votre environnement de développement local à la passerelle de production.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "Vos IDE augmentés par MCP",
          desc: "Notre binaire expose nativement un serveur MCP sur l'entrée/sortie standard (stdio) ou via HTTP/SSE. Les outils de développement IA tels que Claude Code ou Cursor peuvent y faire appel pour connaître en direct l'état des caches.",
          items: ["Enregistrement et arrêt de sessions d'audit par l'agent", "Analyse des statistiques de cache en direct", "Liste et comparaison des coûts réels des modèles d'API"],
          color: "indigo"
        },
        {
          title: "Architecture de sécurité Tiers",
          desc: "Les requêtes d'administration sémantique complexes ou de création de clés d'API virtuelles sont soumises à une vérification rigoureuse par notre Dashboard SaaS Cloud avant exécution.",
          items: ["Isolation stricte par clés virtuelles", "14 outils d'administration disponibles au total", "Mode déconnecté (free tier) ou connecté (SaaS)"],
          color: "violet"
        }
      ],
      videoTitle: "Dialogue direct avec votre agent IDE",
      videoDesc: "Observez un agent dans Claude Desktop interagir directement avec Synapse Proxy. L'agent demande au serveur de lister les clés d'API existantes, d'en créer une nouvelle dotée d'un budget mensuel de $50, puis de lancer une session d'enregistrement.",
      videoAlt: "Démonstration MCP Agent Interaction",
      videoConsoleTitle: "MCP Server Dialogue",
      videoConsoleItems: [
        "Agent: Call 'synapse_create_virtual_key' with budget=$50",
        "Server: key 'sk-opti-x92f...' created successfully.",
        "Agent: \"Excellent. I will use this key for the next requests.\""
      ]
    },
    en: {
      metaTitle: "MCP Server for Cursor & Claude Desktop | Synapse Proxy",
      metaDesc: "Control and monitor your AI gateway directly from your code editors and autonomous agent environments using the MCP protocol.",
      heroBadge: "MCP",
      heroTitle: "Integrated MCP Server",
      heroDesc: "Leverage the power of the Model Context Protocol (MCP) to bridge your local development environment with your production gateway.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "Your IDEs Augmented by MCP",
          desc: "Our binary natively exposes an MCP server over standard input/output (stdio) or HTTP/SSE. AI tools like Claude Code or Cursor can query it to inspect cache stats or route requests.",
          items: ["Audit session start and stop driven by the agent", "Live cache statistics analysis", "Listing and comparing real upstream API costs"],
          color: "indigo"
        },
        {
          title: "Tiered Security Architecture",
          desc: "Complex semantic admin requests or virtual API key creations are verified strictly by our SaaS Cloud Dashboard before execution.",
          items: ["Strict isolation via virtual keys", "14 admin tools available in total", "Offline mode (free tier) or cloud connected (SaaS)"],
          color: "violet"
        }
      ],
      videoTitle: "Direct Dialogue with your IDE Agent",
      videoDesc: "Watch an agent in Claude Desktop interact directly with Synapse Proxy. The agent requests the server to list existing API keys, create a new virtual key with a $50 monthly budget, and start an audit recording.",
      videoAlt: "MCP Agent Interaction Demo",
      videoConsoleTitle: "MCP Server Dialogue",
      videoConsoleItems: [
        "Agent: Call 'synapse_create_virtual_key' with budget=$50",
        "Server: key 'sk-opti-x92f...' created successfully.",
        "Agent: \"Excellent. I will use this key for the next requests.\""
      ]
    }
  },
  costReduction: {
    fr: {
      metaTitle: "Réduction Coûts API LLM pour Startups | Synapse Proxy",
      metaDesc: "Divisez vos factures d'API OpenAI et Anthropic par 5 grâce à notre cache de requêtes et notre pare-feu intelligent.",
      heroBadge: "Startups",
      heroTitle: "Réduction des Coûts",
      heroDesc: "Permettez à votre startup d'IA de passer à l'échelle sans faire exploser votre facture d'API.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "Domptez les coûts d'inférence",
          desc: "Les coûts de développement d'applications à base d'agents autonomes sont souvent prohibitifs. Grâce à notre système de cache intelligent et nos règles de déduplication, évitez de repayer pour des requêtes identiques.",
          items: ["Économisez jusqu'à 80% dès le premier jour", "Outils de simulation de budget par clé", "Intégration de tous les principaux fournisseurs d'IA"],
          color: "emerald"
        },
        {
          title: "Limites Stripe & Alertes de dépenses",
          desc: "Associez des budgets stricts à vos clés d'API virtuelles pour empêcher tout dépassement accidentel. Recevez des notifications par Slack ou email lorsque vos clés approchent de leurs limites.",
          items: ["Budgets mensuels granulaires", "Intégration de facturation Stripe transparente", "Notifications Slack/Email instantanées"],
          color: "amber"
        }
      ],
      videoTitle: "Gestion des abonnements et budgets",
      videoDesc: "Découvrez comment les administrateurs allouent des quotas financiers précis à leurs développeurs et applications. Le panneau montre la gestion des forfaits Stripe (Free, Pro, Scale) et le suivi en direct des tokens.",
      videoAlt: "Démonstration Gestion Facturation Stripe et Budgets",
      videoConsoleTitle: "Stripe Billing Quotas",
      videoConsoleItems: [
        "Plan de Clé: PRO Tier ($5/mois)",
        "Seuil d'Alerte: 80% du budget consommé",
        "Action: Bloquer requêtes au-delà de 20M tokens"
      ]
    },
    en: {
      metaTitle: "LLM API Cost Reduction for Startups | Synapse Proxy",
      metaDesc: "Divide your OpenAI and Anthropic API bills by 5 using our request caching and intelligent firewall.",
      heroBadge: "Startups",
      heroTitle: "Cost Reduction",
      heroDesc: "Scale your AI startup without blowing up your API invoice.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "Tame Inference Costs",
          desc: "Developing autonomous agent apps can be prohibitively expensive. Avoid paying for identical queries during testing or production with our cache systems.",
          items: ["Save up to 80% from day one", "Virtual key budget simulation tools", "Integration with all major AI providers"],
          color: "emerald"
        },
        {
          title: "Stripe Limits & Spend Alerts",
          desc: "Attach strict budgets to your virtual API keys to prevent runaway costs. Get instantly notified via Slack or email when keys near their limits.",
          items: ["Granular monthly budgets", "Seamless Stripe billing integration", "Instant Slack & Email notifications"],
          color: "amber"
        }
      ],
      videoTitle: "Subscription & Budget Management",
      videoDesc: "Discover how administrators allocate precise financial quotas to developers and apps. The panel demonstrates Stripe plan tiering (Free, Pro, Scale) and live token tracking.",
      videoAlt: "Stripe Billing & Budget Management Demo",
      videoConsoleTitle: "Stripe Billing Quotas",
      videoConsoleItems: [
        "Key Plan: PRO Tier ($5/month)",
        "Alert Threshold: 80% of budget consumed",
        "Action: Block requests beyond 20M tokens"
      ]
    }
  },
  agentSafety: {
    fr: {
      metaTitle: "Sécurisation des Agents IA Autonomes | Synapse Proxy",
      metaDesc: "Sécurisez et auditez les actions de vos agents autonomes en temps réel. Évitez les comportements imprévus et tracez les appels d'API.",
      heroBadge: "Sécurité",
      heroTitle: "Sécurité des Agents Autonomes",
      heroDesc: "Gardez le contrôle total sur le comportement d'exécution de vos agents cognitifs en production.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "Audit d'Exécution Complet",
          desc: "Les architectures d'agents complexes rendent le débogage difficile. Synapse Proxy fournit des journaux d'audit complets détaillant les étapes d'exécution, les outils invoqués, la latence et les coûts.",
          items: ["Télémétrie complète et horodatée", "Suivi de l'arbre d'exécution des sous-agents", "Export JSONL / CSV pour conformité d'audit"],
          color: "red"
        },
        {
          title: "Règles de Sécurité Dynamiques",
          desc: "Empêchez vos agents d'accéder à des outils sensibles (écriture de fichiers, exécution système) sans validation humaine ou déclenchez des alertes en cas de comportement suspect.",
          items: ["Blocage par signature de fonction", "Masquage PII automatique des logs", "Mode d'auto-correction intelligent"],
          color: "indigo"
        }
      ],
      videoTitle: "Console de Supervision Super Admin",
      videoDesc: "Regardez comment les administrateurs supervisent les sessions des différents utilisateurs, détectent les erreurs d'appel d'outils et gèrent les droits d'accès. La console affiche en temps réel les journaux d'exécution.",
      videoAlt: "Démonstration Logs Audit Super Admin",
      videoConsoleTitle: "Audit Enforcement Status",
      videoConsoleItems: [
        "Security Override: Enabled",
        "Audit Status: Strict Compliance logging active",
        "Supervision: Active across all agent nodes"
      ]
    },
    en: {
      metaTitle: "Securing Autonomous AI Agents | Synapse Proxy",
      metaDesc: "Secure and audit your autonomous agents in real time. Avoid unexpected behaviors and trace upstream API calls.",
      heroBadge: "Safety",
      heroTitle: "Autonomous Agent Safety",
      heroDesc: "Maintain total control over the execution behavior of your cognitive agents in production.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "Comprehensive Execution Audit",
          desc: "Debugging complex agent architectures is hard. Synapse Proxy provides full audit logs detailing execution turns, tools called, latency, and costs.",
          items: ["Full timestamped telemetry", "Sub-agent execution tree tracing", "JSONL / CSV export for compliance auditing"],
          color: "red"
        },
        {
          title: "Dynamic Security Safeguards",
          desc: "Prevent your agents from accessing critical tools (file writing, system execution) without human review, or trigger alerts on suspicious prompt constructs.",
          items: ["Blocking by function signature", "Automatic PII log masking", "Smart self-recovery instructions"],
          color: "indigo"
        }
      ],
      videoTitle: "Super Admin Supervision Console",
      videoDesc: "Watch how admins monitor active user sessions, catch failed tool executions, and configure access levels. The console displays raw logs in real time.",
      videoAlt: "Super Admin Audit Logs Demo",
      videoConsoleTitle: "Audit Enforcement Status",
      videoConsoleItems: [
        "Security Override: Enabled",
        "Audit Status: Strict Compliance logging active",
        "Supervision: Active across all agent nodes"
      ]
    }
  },
  enterpriseGateway: {
    fr: {
      metaTitle: "Passerelle LLM Souveraine pour Entreprises | Synapse Proxy",
      metaDesc: "Prenez le contrôle total de vos données d'IA. Déployez une passerelle souveraine de cache, de sécurité et d'observabilité sur site ou dans votre cloud.",
      heroBadge: "Entreprise",
      heroTitle: "Passerelle Entreprise Souveraine",
      heroDesc: "Gouvernance, conformité et contrôle des accès pour les charges de travail d'IA générative en entreprise.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "Souveraineté Totale des Données",
          desc: "Contrairement aux proxies d'IA basés sur le cloud qui capturent vos requêtes, Synapse Proxy est open-core. Déployez-le localement au format conteneurisé dans votre propre infrastructure (AWS, Azure, GCP ou bare-metal).",
          items: ["Zéro transit de données sensibles hors de votre réseau", "Cache sémantique ONNX local et privé", "Aucune dépendance externe requise"],
          color: "blue"
        },
        {
          title: "Gouvernance Multi-Clés & Budgets",
          desc: "Créez des clés d'API virtuelles isolées pour chaque département, équipe ou projet. Allouez des budgets stricts, surveillez la consommation en temps réel et appliquez des politiques d'optimisation.",
          items: ["Provisionnement rapide de clés virtuelles", "Rapports d'analytics consolidés", "Intégration LDAP/OAuth pour l'administration"],
          color: "cyan"
        }
      ],
      videoTitle: "Panneau de Gestion Multi-Clés",
      videoDesc: "Regardez comment les administrateurs gèrent et configurent les clés virtuelles à la volée. L'interface offre un aperçu immédiat des budgets alloués, du pare-feu et des performances de cache.",
      videoAlt: "Démonstration Multi Key Management",
      videoConsoleTitle: "Enterprise Compliance",
      videoConsoleItems: [
        "Total Virtual Keys: 42 active keys",
        "Monthly Budget: $4,500 total allocated",
        "Compliance: Audit logger active"
      ]
    },
    en: {
      metaTitle: "Sovereign Enterprise LLM Gateway | Synapse Proxy",
      metaDesc: "Take total control of your AI data. Deploy a sovereign caching, security, and observability gateway on-premise or in your cloud.",
      heroBadge: "Enterprise",
      heroTitle: "Sovereign Enterprise Gateway",
      heroDesc: "Governance, compliance, and access control for generative AI workloads in enterprise environments.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "Complete Data Sovereignty",
          desc: "Unlike cloud-based AI proxies that capture your queries, Synapse Proxy is open-core. Deploy it containerized in your own cloud infrastructure (AWS, Azure, GCP or bare-metal).",
          items: ["Zero sensitive data transit outside your network", "Private local ONNX semantic cache", "No external dependencies required"],
          color: "blue"
        },
        {
          title: "Multi-Key Governance & Budgets",
          desc: "Create isolated virtual API keys for each department, team, or project. Allocate strict budgets, track spend in real time, and enforce caching rules.",
          items: ["Fast virtual key provisioning", "Consolidated analytics reporting", "LDAP/OAuth integration for admin logins"],
          color: "cyan"
        }
      ],
      videoTitle: "Multi-Key Management Dashboard",
      videoDesc: "Watch how admins provision and configure virtual keys on the fly. The UI offers an immediate view of allocated budgets, firewall status, and per-key cache performances.",
      videoAlt: "Multi Key Management Demo",
      videoConsoleTitle: "Enterprise Compliance",
      videoConsoleItems: [
        "Total Virtual Keys: 42 active keys",
        "Monthly Budget: $4,500 total allocated",
        "Compliance: Audit logger active"
      ]
    }
  },
  litellm: {
    fr: {
      metaTitle: "Alternative à LiteLLM | Synapse Proxy vs LiteLLM",
      metaDesc: "Découvrez en quoi Synapse Proxy se distingue de LiteLLM par son architecture open-core avec cache sémantique local intégré (ONNX/VSS) et son Agentic Firewall.",
      heroBadge: "vs LiteLLM",
      heroTitle: "Synapse Proxy vs LiteLLM",
      heroDesc: "Pourquoi choisir Synapse Proxy pour vos infrastructures d'agents autonomes et vos politiques de cache.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [],
      videoTitle: "Caching sémantique comparé en direct",
      videoDesc: "LiteLLM gère la redirection simple des requêtes, mais Synapse Proxy agit comme une passerelle d'optimisation intelligente. Regardez comment notre Playground intercepte instantanément les invites sémantiquement similaires.",
      videoAlt: "Démonstration Playground Cache Hit Comparaison",
      videoConsoleTitle: "Comparison Metrics",
      videoConsoleItems: [
        "LiteLLM: Redirige à 100% vers l'amont (Facturation complète)",
        "Synapse Proxy: Cache L2 Hit (Économie immédiate de $0.00045)"
      ],
      table: {
        headers: ["Fonctionnalité", "Synapse Proxy", "LiteLLM"],
        rows: [
          { feature: "Cache Sémantique Local (ONNX + Redis VSS)", synapse: "Oui (Intégré/Gratuit)", other: "Limité (requiert API tierce)" },
          { feature: "Pare-feu Agentique (Anti-Boucle d'outils)", synapse: "Oui (Interception sémantique)", other: "Non (redirection simple)" },
          { feature: "Compression de Contexte (L3)", synapse: "Oui (Network Gateway Level)", other: "Non" },
          { feature: "Serveur MCP Intégré (Cursor/Claude)", synapse: "Oui (14 outils)", other: "Non" },
          { feature: "Contrôle des Budgets par Clé", synapse: "Oui (Stripe + local rules)", other: "Oui" }
        ]
      }
    },
    en: {
      metaTitle: "LiteLLM Alternative | Synapse Proxy vs LiteLLM",
      metaDesc: "Learn how Synapse Proxy compares to LiteLLM with its open-core architecture, built-in local semantic caching (ONNX/VSS), and Agentic Firewall.",
      heroBadge: "vs LiteLLM",
      heroTitle: "Synapse Proxy vs LiteLLM",
      heroDesc: "Why choose Synapse Proxy for your autonomous agent infrastructure and caching policies.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [],
      videoTitle: "Live Semantic Caching Comparison",
      videoDesc: "LiteLLM handles simple request routing, but Synapse Proxy acts as an intelligent optimization gateway. Watch how our Playground intercepts semantically similar prompts to save costs.",
      videoAlt: "Playground Cache Hit Comparison Demo",
      videoConsoleTitle: "Comparison Metrics",
      videoConsoleItems: [
        "LiteLLM: Routes 100% to upstream provider (Full Billing)",
        "Synapse Proxy: Cache L2 Hit (Immediate saving of $0.00045)"
      ],
      table: {
        headers: ["Feature", "Synapse Proxy", "LiteLLM"],
        rows: [
          { feature: "Local Semantic Caching (ONNX + Redis VSS)", synapse: "Yes (Built-in/Free)", other: "Limited (requires third-party API)" },
          { feature: "Agentic Firewall (Anti-Loop)", synapse: "Yes (Semantic interception)", other: "No (simple routing only)" },
          { feature: "Context Compression (L3)", synapse: "Yes (Network Gateway Level)", other: "No" },
          { feature: "Integrated MCP Server", synapse: "Yes (14 tools built-in)", other: "No" },
          { feature: "Per-Key Budget Controls", synapse: "Yes (Stripe + local rules)", other: "Yes" }
        ]
      }
    }
  },
  portkey: {
    fr: {
      metaTitle: "Alternative à Portkey | Synapse Proxy vs Portkey",
      metaDesc: "Découvrez pourquoi Synapse Proxy est l'alternative souveraine à Portkey pour les entreprises qui exigent un déploiement 100% sur site et privé.",
      heroBadge: "vs Portkey",
      heroTitle: "Synapse Proxy vs Portkey.ai",
      heroDesc: "Souveraineté des données d'entreprise et cache sémantique local vs solutions Cloud SaaS propriétaires.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [],
      videoTitle: "Gestion souveraine des clés et logs",
      videoDesc: "Alors que Portkey impose d'envoyer vos clés d'API et vos logs sur leur cloud tiers, Synapse Proxy vous permet de garder un contrôle absolu. Regardez comment configurer en quelques secondes votre trousseau de clés virtuelles entièrement chez vous.",
      videoAlt: "Démonstration Gestion Clés Privées Comparaison",
      videoConsoleTitle: "Compliance Routing",
      videoConsoleItems: [
        "Portkey: Transit Cloud Obligatoire (RGPD à valider)",
        "Synapse Proxy: Déploiement Conteneur Privé local (100% Souverain)"
      ],
      table: {
        headers: ["Critère", "Synapse Proxy", "Portkey.ai"],
        rows: [
          { feature: "Souveraineté / Hébergement Local", synapse: "100% Souverain (Self-Hosted/MIT)", other: "Principalement SaaS (Cloud Propriétaire)" },
          { feature: "Cache Sémantique Privé (ONNX local)", synapse: "Oui (sans transit de données)", other: "Nécessite le transit sur leurs serveurs" },
          { feature: "Pare-feu pour boucles infinies", synapse: "Oui (Agentic Firewall)", other: "Non (journalisation basique uniquement)" },
          { feature: "Facturation Multi-tenant intégrée", synapse: "Oui (Stripe)", other: "Oui" }
        ]
      }
    },
    en: {
      metaTitle: "Portkey Alternative | Synapse Proxy vs Portkey",
      metaDesc: "Discover why Synapse Proxy is the sovereign alternative to Portkey for companies requiring 100% on-premise and private deployments.",
      heroBadge: "vs Portkey",
      heroTitle: "Synapse Proxy vs Portkey.ai",
      heroDesc: "Enterprise data sovereignty and local semantic caching vs proprietary SaaS Cloud solutions.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [],
      videoTitle: "Sovereign Key & Log Management",
      videoDesc: "While Portkey mandates sending your API keys and log streams to their cloud server, Synapse Proxy lets you keep absolute control. See how easy it is to set up virtual keys locally.",
      videoAlt: "Private Key Management Comparison Demo",
      videoConsoleTitle: "Compliance Routing",
      videoConsoleItems: [
        "Portkey: Mandatory Cloud Transit (GDPR concerns)",
        "Synapse Proxy: Local Private Container Deployment (100% Sovereign)"
      ],
      table: {
        headers: ["Criterion", "Synapse Proxy", "Portkey.ai"],
        rows: [
          { feature: "Sovereignty / Local Hosting", synapse: "100% Sovereign (Self-Hosted/MIT)", other: "Mostly SaaS (Proprietary Cloud)" },
          { feature: "Private Semantic Cache (Local ONNX)", synapse: "Yes (no data leakage)", other: "Requires routing payloads to their servers" },
          { feature: "Infinite Loop Firewall", synapse: "Yes (Agentic Firewall)", other: "No (basic logging only)" },
          { feature: "Integrated Multi-tenant Billing", synapse: "Yes (Stripe)", other: "Yes" }
        ]
      }
    }
  },
  llmlingua: {
    fr: {
      metaTitle: "Compression de Tokens LLM | Synapse Proxy vs LLMLingua",
      metaDesc: "Découvrez en quoi la compression de prompt au niveau de la passerelle de Synapse Proxy est plus simple et performante que le pruning applicatif avec LLMLingua.",
      heroBadge: "vs LLMLingua",
      heroTitle: "Synapse Proxy vs LLMLingua",
      heroDesc: "Pourquoi intégrer la compression de contexte au niveau réseau plutôt que de surcharger votre application client.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "Synapse Proxy (Gateway Level)",
          desc: "La compression s'effectue directement sur le flux réseau transitant par la passerelle de proxy. Tout client HTTP standard (OpenAI SDK, Python requests) en bénéficie sans réécrire le code.",
          items: ["Zéro ligne de code supplémentaire côté client", "Cache L1/L2 directement chaîné avec la compression", "Latence de traitement masquée sur le réseau"],
          color: "cyan"
        },
        {
          title: "LLMLingua (Application Level)",
          desc: "Nécessite d'installer de lourdes dépendances Python localement, de charger le modèle de compression en mémoire GPU côté client, et de formater manuellement chaque payload.",
          items: ["Intégration lourde et dépendante de Python", "Consommation de ressources de calcul locales importante", "Difficile à déployer de manière uniforme en équipe"],
          color: "purple"
        }
      ],
      videoTitle: "Visualisation en direct du taux de compression",
      videoDesc: "Regardez notre passerelle réseau compresser dynamiquement les historiques de prompts volumineux. Les graphiques d'analyse mettent en évidence les gains de tokens et d'argent.",
      videoAlt: "Démonstration Analytics Token Compression Comparaison",
      videoConsoleTitle: "Compression Savings",
      videoConsoleItems: [
        "Synapse Proxy: Active compression transparently",
        "Savings Rate: up to 45% token pruning verified"
      ]
    },
    en: {
      metaTitle: "LLM Token Compression | Synapse Proxy vs LLMLingua",
      metaDesc: "Discover why prompt compression at the Synapse Proxy gateway level is simpler and faster than application-level pruning with LLMLingua.",
      heroBadge: "vs LLMLingua",
      heroTitle: "Synapse Proxy vs LLMLingua",
      heroDesc: "Why integrate context compression at the network layer rather than overloading your client application.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "Synapse Proxy (Gateway Level)",
          desc: "Compression is performed directly on the network streams passing through the gateway. Any standard HTTP client benefits instantly without rewriting any logic.",
          items: ["Zero lines of client code needed", "L1/L2 Cache chained directly with compression", "Processing latency hidden over the wire"],
          color: "cyan"
        },
        {
          title: "LLMLingua (Application Level)",
          desc: "Requires installing heavy Python dependencies locally, loading a pruning model into local GPU memory, and manually formatting payloads before sending.",
          items: ["Heavy integration tied to Python environment", "High local compute resource consumption", "Hard to enforce consistently across developers"],
          color: "purple"
        }
      ],
      videoTitle: "Live Compression Rate Visualization",
      videoDesc: "Watch our network gateway compress large prompt histories on the fly. The analytics charts show token and financial gains for each agent turn.",
      videoAlt: "Token Compression Analytics Demo",
      videoConsoleTitle: "Compression Savings",
      videoConsoleItems: [
        "Synapse Proxy: Active compression transparently",
        "Savings Rate: up to 45% token pruning verified"
      ]
    }
  },
  cgv: {
    fr: {
      metaTitle: "Conditions Générales de Vente | Synapse Proxy",
      metaDesc: "Conditions Générales de Vente et d'Utilisation de Synapse Proxy.",
      heroBadge: "CGV",
      heroTitle: "Conditions Générales de Vente",
      heroDesc: "Veuillez lire attentivement nos conditions générales de vente et d'utilisation.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "1. Objet",
          desc: "Les présentes Conditions Générales ont pour objet de définir les modalités de mise à disposition des services de Synapse Proxy.",
          items: [],
          color: "emerald"
        },
        {
          title: "2. Tarifs et Paiement",
          desc: "Les services sont facturés selon le plan choisi. Les paiements sont traités de manière sécurisée via Stripe.",
          items: [],
          color: "teal"
        }
      ],
      videoTitle: "",
      videoDesc: "",
      videoAlt: ""
    },
    en: {
      metaTitle: "Terms of Sale | Synapse Proxy",
      metaDesc: "Terms of Sale and Use for Synapse Proxy.",
      heroBadge: "Terms",
      heroTitle: "Terms of Sale",
      heroDesc: "Please read our terms of sale and use carefully.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "1. Purpose",
          desc: "These General Terms define the terms and conditions for using Synapse Proxy services.",
          items: [],
          color: "emerald"
        },
        {
          title: "2. Pricing and Payment",
          desc: "Services are billed according to the selected plan. Payments are securely processed via Stripe.",
          items: [],
          color: "teal"
        }
      ],
      videoTitle: "",
      videoDesc: "",
      videoAlt: ""
    }
  },
  privacy: {
    fr: {
      metaTitle: "Politique de Confidentialité | Synapse Proxy",
      metaDesc: "Politique de protection des données personnelles de Synapse Proxy.",
      heroBadge: "Confidentialité",
      heroTitle: "Politique de Confidentialité",
      heroDesc: "Nous accordons une importance capitale à la protection de vos données personnelles.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "1. Collecte des Données",
          desc: "Nous collectons uniquement les informations nécessaires au bon fonctionnement de la passerelle et du tableau de bord.",
          items: [
            "Adresses email pour la création de compte",
            "Clés d'API virtuelles pour l'authentification",
            "Métadonnées d'usage et statistiques de tokens"
          ],
          color: "blue"
        },
        {
          title: "2. Utilisation des Données",
          desc: "Vos données ne sont jamais vendues ou partagées avec des tiers à des fins publicitaires. Notre mode Zero-Log garantit la confidentialité totale de vos prompts.",
          items: [],
          color: "cyan"
        }
      ],
      videoTitle: "",
      videoDesc: "",
      videoAlt: ""
    },
    en: {
      metaTitle: "Privacy Policy | Synapse Proxy",
      metaDesc: "Privacy policy and data protection for Synapse Proxy.",
      heroBadge: "Privacy",
      heroTitle: "Privacy Policy",
      heroDesc: "We take the protection of your personal data very seriously.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "1. Data Collection",
          desc: "We only collect information necessary for the operation of the gateway and dashboard.",
          items: [
            "Email addresses for account creation",
            "Virtual API keys for authentication",
            "Usage metadata and token statistics"
          ],
          color: "blue"
        },
        {
          title: "2. Data Usage",
          desc: "Your data is never sold or shared with third parties for advertising. Our Zero-Log mode ensures total privacy of your prompts.",
          items: [],
          color: "cyan"
        }
      ],
      videoTitle: "",
      videoDesc: "",
      videoAlt: ""
    }
  },
  legal: {
    fr: {
      metaTitle: "Mentions Légales | Synapse Proxy",
      metaDesc: "Mentions légales de Synapse Proxy.",
      heroBadge: "Mentions Légales",
      heroTitle: "Mentions Légales",
      heroDesc: "Informations légales concernant l'éditeur et l'hébergeur du site.",
      backBtn: "Retour au Tableau de Bord",
      dashboardBtn: "Tableau de Bord",
      sections: [
        {
          title: "1. Éditeur du Site",
          desc: "Synapse Proxy est édité par Optitoken SAS, au capital de 10 000 €, immatriculée au RCS sous le numéro 123 456 789. Siège social : Paris, France.",
          items: [],
          color: "purple"
        },
        {
          title: "2. Hébergement",
          desc: "Le site et la passerelle sont hébergés par Hetzner Online GmbH, Allemagne.",
          items: [],
          color: "indigo"
        }
      ],
      videoTitle: "",
      videoDesc: "",
      videoAlt: ""
    },
    en: {
      metaTitle: "Legal Notice | Synapse Proxy",
      metaDesc: "Legal notice for Synapse Proxy.",
      heroBadge: "Legal",
      heroTitle: "Legal Notice",
      heroDesc: "Legal information regarding the site publisher and host.",
      backBtn: "Back to Dashboard",
      dashboardBtn: "Dashboard",
      sections: [
        {
          title: "1. Publisher",
          desc: "Synapse Proxy is published by Optitoken SAS, capital of €10,000, registered with the RCS under number 123 456 789. Registered office: Paris, France.",
          items: [],
          color: "purple"
        },
        {
          title: "2. Hosting",
          desc: "The website and gateway are hosted by Hetzner Online GmbH, Germany.",
          items: [],
          color: "indigo"
        }
      ],
      videoTitle: "",
      videoDesc: "",
      videoAlt: ""
    }
  }
};

export async function getTranslation(pageKey: string, lang: Language): Promise<TranslationItem> {
  try {
    const record = await prisma.landingPageContent.findUnique({
      where: {
        pageKey_lang: {
          pageKey,
          lang,
        },
      },
    });
    if (record && record.content) {
      return JSON.parse(record.content) as TranslationItem;
    }
  } catch (error) {
    console.error(`Error fetching dynamic translation for ${pageKey}/${lang}:`, error);
  }

  // Fallback to static translations
  const pageTranslations = translations[pageKey];
  if (pageTranslations && pageTranslations[lang]) {
    return pageTranslations[lang];
  }

  // Final emergency fallback to empty/default structure
  return {
    metaTitle: "",
    metaDesc: "",
    heroBadge: "",
    heroTitle: "",
    heroDesc: "",
    backBtn: lang === "fr" ? "Retour" : "Back",
    dashboardBtn: lang === "fr" ? "Tableau de Bord" : "Dashboard",
    sections: [],
    videoTitle: "",
    videoDesc: "",
    videoAlt: "",
  };
}

