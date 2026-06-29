======================================================================
              Synapse Proxy Local Client - Quick Start Guide
======================================================================

Synapse Proxy is a local LLM caching and optimization proxy. It starts
a local proxy at port 8080 and a control dashboard at port 4321.

----------------------------------------------------------------------
1. WINDOWS DEFENDER WARNING (False Positive)
----------------------------------------------------------------------
Because this executable is compiled directly from source and is not
digitally signed with an expensive corporate certificate, Windows Defender
will block it upon download.

To allow and run the application:
1. Open Windows Security (Sécurité Windows).
2. Go to "Virus & threat protection" (Protection contre les virus et menaces).
3. Click on "Protection history" (Historique des protections).
4. Locate the blocked threat for "synapse-local.exe".
5. Click on "Actions" -> select "Allow on device" (Autoriser sur l'appareil)
   or "Restore" (Restaurer).

----------------------------------------------------------------------
2. HOW TO LAUNCH
----------------------------------------------------------------------
Simply double-click "synapse-local.exe" or run it from the console:
  .\synapse-local.exe

It will automatically initialize its local database "synapse_local.db"
in the same folder. No installer is required.

----------------------------------------------------------------------
3. CONFIGURATION & ACCESS
----------------------------------------------------------------------
- Dashboard Access: Open http://localhost:4321 in your browser.
- Proxy Endpoint: Configure your tools to point to:
  http://localhost:8080/v1

To activate your premium quotas, copy your license key from the
cloud settings page and paste it into the "Local Client License"
field on your local settings tab (http://localhost:4321/settings).
======================================================================
