# Conception Client Local (.exe) & Système de Licensing

Ce document détaille l'architecture technique d'un exécutable client Windows (`.exe`) ultra-léger pour **Synapse Proxy**, son intégration de licences et la gestion des quotas de compression.

---

## 1. Avis Stratégique & Analyse (Synthèse)

* **Excellente idée UX & Business** : Le passage d'une stack complexe nécessitant Docker et plusieurs serveurs à un simple exécutable autonome (double-clic) déverrouille l'adoption par 100% des développeurs.
* **Souveraineté (B2B)** : Les entreprises réticentes au cloud peuvent faire tourner Synapse en local.
* **Le Défi** : Packager Postgres + Redis-stack + Docker dans un `.exe` est intenable (taille >2 Go). La solution est d'implémenter une version **"Lite"** autonome sans dépendances externes.

---

## 2. Spécifications du Client "Ultra-Léger"

Afin de maintenir une taille inférieure à **50 Mo** et un démarrage instantané :
1. **Base de données locales** : Remplacement de PostgreSQL par **SQLite** (embarqué dans le binaire).
2. **Cache & Index Vectoriel (L1/L2)** : Remplacement de Redis Stack par une bibliothèque de base de données clé-valeur intégrée en Go (ex. **BoltDB** ou **BadgerDB**) et un index vectoriel léger en mémoire.
3. **Interface Dashboard** : Compilation du Dashboard Next.js en fichiers statiques exportés (`next export`), embarqués dans le binaire Go avec `go:embed` et servis sur `http://localhost:3000/`.

---

## 3. Grille des Licences et Quotas

Conformément aux décisions d'évolutions, voici la répartition des niveaux d'accès :

| Niveau | Quota Mensuel (Tokens Compressés) | Caches Disponibles | Fonctionnalités Incluses |
| :--- | :---: | :---: | :--- |
| **FREE** | **10 Millions** | **L1 + L2 + L3** | Filtrage PII, Détecteur de boucle. |
| **PRO** | **50 Millions** | **L1 + L2 + L3** | Compresseurs avancés (AST, Diff, SmartCrusher). |
| **ENTERPRISE** | **Illimité** | **L1 + L2 + L3** | Synchronisation des logs d'équipe, Support dédié. |

---

## 4. DRM & Validation des Licences

Pour éviter qu'un utilisateur ne modifie ou ne craque le quota en local :
1. **Validation cryptographique** : La licence entrée dans le client local contient une signature asymétrique (clé publique intégrée dans le `.exe`) vérifiée localement au démarrage.
2. **Synchronisation Hybride (Heartbeat)** : Toutes les 10 minutes ou tous les 100 000 tokens compressés, le client local envoie un résumé chiffré de sa consommation à l'API Cloud de Synapse. Si le serveur cloud répond que le quota est dépassé, le client local désactive la compression.
3. **Tolérance Offline** : Le client local tolère une déconnexion internet temporaire de 24h à 48h avant de bloquer l'usage, offrant la flexibilité requise pour le travail nomade.
