# 🚀 Guide de Déploiement : OptiToken sur Hetzner Cloud

Ce guide pas-à-pas vous explique comment déployer l'architecture complète d'OptiToken (Next.js, Proxy Go, ONNX Python, Redis, Postgres) sur un VPS Hetzner de A à Z.

---

## 1. Commande du Serveur sur Hetzner Cloud

1. Rendez-vous sur [Hetzner Cloud](https://console.hetzner.cloud/) et créez un compte / projet.
2. Cliquez sur **Add Server**.
3. **Location :** Choisissez la localisation la plus proche de vos clients (ex: *Falkenstein* ou *Paris*).
4. **Image :** `Ubuntu 22.04` ou `Ubuntu 24.04`.
5. **Type :** `Shared vCPU` -> `x86` (Important : gardez x86 et non ARM64 pour assurer la compatibilité totale de l'Embedder ONNX).
   - Choisissez l'instance **CX32** (4 vCPU, 8 Go de RAM, ~7.50€/mois). C'est le ratio parfait.
6. **Networking :** Laissez *Public IPv4* coché.
7. **SSH keys :** Ajoutez votre clé SSH publique (fortement recommandé) ou laissez vide pour recevoir un mot de passe root par email.
8. Cliquez sur **Create & Buy now**. En 10 secondes, votre serveur a une adresse IP publique.

---

## 2. Préparation du Nom de Domaine

Chez votre *registrar* (o2switch, OVH, etc.), créez **deux enregistrements A** pointant vers l'adresse IP de votre serveur Hetzner :
- `app.votre-domaine.com` ➔ Pointant vers l'IP Hetzner (pour le Dashboard Next.js).
- `api.votre-domaine.com` ➔ Pointant vers l'IP Hetzner (pour le Proxy Go).

---

## 3. Configuration Initiale du Serveur

Ouvrez un terminal sur votre machine locale et connectez-vous au serveur :
```bash
ssh root@<IP_DU_SERVEUR_HETZNER>
```

Mettez à jour le système et installez les outils de base :
```bash
apt update && apt upgrade -y
apt install -y git curl wget unzip nano nginx certbot python3-certbot-nginx
```

Installez **Docker** et **Docker Compose** :
```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
```

---

## 4. Installation d'OptiToken

Transférez votre code sur le serveur. Si vous utilisez GitHub/GitLab (privé), clonez le dépôt. Sinon, utilisez `scp` depuis votre machine locale.
Ici, nous supposerons que le code est dans le dossier `/root/Optitoken`.

```bash
cd /root/Optitoken
```

### Configuration des variables d'environnement
Il faut adapter vos fichiers `.env` pour la production. En particulier pour le dashboard Next.js (`dashboard/.env`), assurez-vous que l'URL pointe vers votre sous-domaine API.

```env
# dashboard/.env
NEXT_PUBLIC_API_URL=https://api.votre-domaine.com
# N'oubliez pas les variables NEXTAUTH_URL, DATABASE_URL, etc.
```

### Lancement des conteneurs
Construisez et lancez toute l'infrastructure (Postgres, Redis, ONNX, Proxy, Dashboard) :

```bash
docker compose up -d --build
```
> 💡 *Cette étape prendra quelques minutes (surtout pour télécharger les dépendances Go et les modèles Python ONNX).*

Vérifiez que tout tourne correctement avec `docker compose ps`.

---

## 5. Configuration du Reverse Proxy (Nginx) et SSL (HTTPS)

Vos conteneurs tournent en local sur les ports `3000` (Dashboard) et `8080` (Proxy). Il faut exposer cela proprement sur le web avec du SSL (HTTPS).

Créez un fichier de configuration Nginx :
```bash
nano /etc/nginx/sites-available/optitoken
```

Collez cette configuration (en remplaçant `votre-domaine.com`) :

```nginx
# Configuration pour le Dashboard
server {
    listen 80;
    server_name app.votre-domaine.com;

    location / {
        proxy_pass http://localhost:3000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
    }
}

# Configuration pour le Proxy API
server {
    listen 80;
    server_name api.votre-domaine.com;

    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        
        # Très important pour le Server-Sent Events (SSE) :
        proxy_set_header Connection '';
        chunked_transfer_encoding off;
        proxy_buffering off;
        proxy_cache off;
        
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Sauvegardez (`Ctrl+O`, `Entrée`, `Ctrl+X`), activez la configuration et redémarrez Nginx :
```bash
ln -s /etc/nginx/sites-available/optitoken /etc/nginx/sites-enabled/
nginx -t
systemctl restart nginx
```

### Activer le HTTPS Gratuit avec Certbot
Lancez cette commande et suivez les instructions à l'écran :
```bash
certbot --nginx -d app.votre-domaine.com -d api.votre-domaine.com
```

Certbot va automatiquement modifier Nginx pour activer le SSL et forcer la redirection HTTP -> HTTPS.

---

## 🎉 C'est fini !
Votre SaaS OptiToken est maintenant déployé de manière professionnelle !

- **Vos utilisateurs** se connectent sur : `https://app.votre-domaine.com`
- **Leurs agents (Hermes, etc.)** pointent sur l'URL de base : `https://api.votre-domaine.com/v1`

> [!TIP]
> Si vous mettez à jour votre code en local, il vous suffira de faire un `git pull` sur le serveur (ou de copier les nouveaux fichiers), puis de lancer `docker compose up -d --build proxy` (ou le composant mis à jour) pour appliquer les changements sans interruption de service.
