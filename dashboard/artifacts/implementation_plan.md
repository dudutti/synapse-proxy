# Implémentation du système d'upload de médias CMS

Ce plan détaille la mise en place d'un système d'upload direct (vidéos MP4, WebP, images) depuis le tableau de bord d'administration, avec gestion de la persistance des fichiers et de la taille d'affichage.

## User Review Required

> [!WARNING]
> Puisque l'application est déployée via un conteneur Docker (recréé à chaque `node deploy-full.js`), les fichiers sauvegardés localement dans le conteneur seraient normalement détruits à chaque mise à jour.
> **Pour éviter cela**, je vais configurer un **Volume Docker** (`dashboard_uploads`) dans `docker-compose.prod.yml`. Les médias uploadés seront ainsi stockés de manière permanente sur le serveur, indépendamment des redéploiements.

## Proposed Changes

### 1. Configuration Docker (Persistance)

#### [MODIFY] docker-compose.prod.yml
- Ajout d'un volume nommé `dashboard_uploads` au service `dashboard`.
- Montage du volume sur `/app/public/uploads` pour que Next.js serve ces fichiers statiquement.

### 2. Backend (API d'upload)

#### [NEW] dashboard/app/api/admin/upload/route.ts
- Création d'une nouvelle route API sécurisée (réservée aux `SUPERADMIN`).
- Traitement des requêtes `multipart/form-data`.
- Sauvegarde sécurisée du fichier dans `public/uploads/` avec un horodatage pour éviter les conflits de noms.
- Renvoi de l'URL publique (ex: `/uploads/1718900000-demo.mp4`).

### 3. Schéma de données

#### [MODIFY] dashboard/lib/translations.ts
- Ajout de `videoUrl?: string` dans l'interface globale `TranslationItem`.
- Ajout de `mediaUrl?: string` et `mediaSize?: "small" | "medium" | "large" | "full"` dans l'interface `Section`.

### 4. Interface d'Administration (CMS)

#### [MODIFY] dashboard/app/admin/content/page.tsx
- **Composant File Uploader** : Création d'un bouton d'upload avec indicateur de chargement. Lorsqu'un fichier est sélectionné, il est envoyé à l'API et l'URL renvoyée remplit automatiquement le champ texte.
- Ajout d'un champ d'upload pour la vidéo principale de la page (`videoUrl`).
- Ajout d'un champ d'upload et d'un sélecteur de taille (`mediaSize`) pour chaque section/carte.

### 5. Intégration côté client (Pages publiques)

#### [MODIFY] dashboard/app/features/caching/page.tsx (et autres pages concernées)
- Remplacement des sources vidéo codées en dur (ex: `src="/caching_telemetry.webp"`) par le champ dynamique `t.videoUrl || "/caching_telemetry.webp"`.
- Ajout d'une logique d'affichage dans le rendu des `sections` pour afficher l'image/vidéo `mediaUrl` avec les classes Tailwind correspondant à la `mediaSize` choisie.

## Verification Plan

### Manual Verification
1. Aller sur `/admin/content` et uploader une courte vidéo MP4 dans une section.
2. Vérifier que la vidéo apparaît bien dans la prévisualisation et que le champ texte contient `/uploads/votre-video.mp4`.
3. Sauvegarder la page.
4. Visiter la page publique modifiée et constater que le média s'affiche correctement avec la taille sélectionnée.
5. Exécuter un redéploiement (`node deploy-full.js`) et vérifier que la vidéo uplaodée n'a pas disparu.
