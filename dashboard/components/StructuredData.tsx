import React from "react";

export default function StructuredData() {
  const jsonLd = {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "SoftwareApplication",
        "@id": "https://synapse-proxy.com/#software",
        "name": "Synapse Proxy",
        "url": "https://synapse-proxy.com",
        "applicationCategory": "DeveloperApplication",
        "operatingSystem": "Linux, Windows, macOS",
        "description": "Sovereign AI Gateway and Semantic Cache Proxy for LLM APIs.",
        "offers": {
          "@type": "Offer",
          "price": "5.00",
          "priceCurrency": "USD",
          "priceSpecification": {
            "@type": "UnitPriceSpecification",
            "price": "5.00",
            "priceCurrency": "USD",
            "referenceQuantity": {
              "@type": "QuantitativeValue",
              "value": "1",
              "unitCode": "MON"
            }
          }
        }
      },
      {
        "@type": "Product",
        "@id": "https://synapse-proxy.com/#product",
        "name": "Synapse Proxy",
        "description": "Sovereign AI gateway and reverse proxy with semantic caching (L1/L2/L3) and Agentic Firewall.",
        "brand": {
          "@type": "Brand",
          "name": "Optitoken"
        }
      },
      {
        "@type": "FAQPage",
        "@id": "https://synapse-proxy.com/#faq",
        "mainEntity": [
          {
            "@type": "Question",
            "name": "Comment Synapse Proxy réduit-il les coûts d'API LLM ?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "Synapse Proxy réduit les coûts d'API de LLM jusqu'à 80% grâce à un cache triple niveau : L1 (cache exact ultrarapide en moins de 5ms), L2 (cache sémantique local avec recherche vectorielle sur site ONNX/Redis VSS), et L3 (compression et élagage intelligent des fenêtres de contexte d'historique)."
            }
          },
          {
            "@type": "Question",
            "name": "Qu'est-ce que l'Agentic Firewall et la détection de boucle ?",
            "acceptedAnswer": {
              "@type": "Answer",
              "text": "L'Agentic Firewall est un système de sécurité qui analyse les appels d'outils répétés des agents IA autonomes. S'il détecte une boucle répétitive, il l'intercepte et injecte une invite d'auto-correction système pour forcer l'agent à corriger ses paramètres."
            }
          }
        ]
      }
    ]
  };

  return (
    <script
      type="application/ld+json"
      dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
    />
  );
}
