# legacy-map

Cartographie automatique des flux d'exécution PHP/Symfony via XDebug + LLM.

Transforme les traces XDebug brutes en call trees exploitables par Claude, Copilot, ou n'importe quel LLM via MCP.

## Le problème

Sur une codebase legacy PHP :
- Personne ne sait exactement ce que fait le code
- La doc n'existe pas ou est obsolète
- Les flux métier sont incompréhensibles sans guide

## La solution

1. Tu cliques dans ton app Symfony
2. XDebug capture la trace d'exécution
3. legacy-map filtre le bruit (99.9% des appels framework éliminés)
4. Tu demandes à Claude : "Qu'est-ce qui vient de se passer ?"

## Quick Start

### Installation

```bash
go install github.com/drkilla/legacy-map@latest
```

### Setup (30 secondes)

```bash
cd /ton/projet/symfony
legacy-map init
# Suis les instructions affichées
```

### Utilisation

```bash
# Lance le MCP server
legacy-map serve ./xdebug-traces

# Ou branche directement Claude Code
legacy-map setup-mcp ./xdebug-traces
```

Puis dans Claude Code :
> "Retrace moi ce qui se passe quand je POST sur /api/clients"

Claude déclenche automatiquement la requête, capture la trace XDebug, et t'explique le flux.

## Installation sans Go

Télécharge le binaire depuis [Releases](https://github.com/drkilla/legacy-map/releases) :

```bash
# Linux
curl -L https://github.com/drkilla/legacy-map/releases/latest/download/legacy-map-linux-amd64 -o legacy-map
chmod +x legacy-map
sudo mv legacy-map /usr/local/bin/

# macOS
curl -L https://github.com/drkilla/legacy-map/releases/latest/download/legacy-map-darwin-arm64 -o legacy-map
chmod +x legacy-map
sudo mv legacy-map /usr/local/bin/
```

## Résultats réels

| Trace | Appels bruts | Après filtrage | Réduction |
|-------|-------------|----------------|-----------|
| POST /api/admin/clients | 133,000 | 43 | 99.97% |
| POST /api/auth/login | 102,272 | 29 | 99.97% |
| GET /api/admin/cases | 133,000 | 43 | 99.97% |

## Commandes

```bash
legacy-map init                  # Setup XDebug + dossier traces
legacy-map parse <file.xt>       # Parse une trace → JSON stdout
legacy-map watch <dir>           # Surveille et parse en continu
legacy-map serve <dir>           # Watcher + MCP server
legacy-map setup-mcp <dir>       # Configure Claude Code
```

## Comment ça marche

Trois couches de filtrage transforment des centaines de milliers d'appels en quelques dizaines de nœuds :

1. **Fonctions PHP internes** — `strlen`, `array_map`, `sprintf`, etc. sont éliminées
2. **Namespaces vendor** — `Symfony\`, `Doctrine\`, `Twig\`, `Monolog\`, `Psr\`, `Composer\`, etc. sont exclus
3. **Collapse sous-arbres** — quand du code `App\` appelle du vendor, seul le point d'entrée est conservé (pas ses centaines d'appels internes)

En plus : les appels répétés consécutifs sont mergés (`call_count`), les external calls dédupliqués, et les valeurs tronquées à 200 chars.

## MCP Tools

- **`trigger_trace(url, method, body, headers)`** — déclenche une requête HTTP avec XDebug, attend la trace, et retourne le call tree. **La killer feature** : Claude fait tout sans quitter le terminal.
- **`list_traces`** — liste les traces en mémoire (URI, durée, stats)
- **`get_last_trace(n)`** — les N dernières traces avec call tree complet
- **`get_trace_by_uri(uri)`** — traces matchant une URI (recherche partielle)

## Configuration

```bash
# Changer le namespace applicatif (défaut: App\)
legacy-map parse --app-ns=Acme\\ trace.xt

# Exclure des namespaces supplémentaires
legacy-map parse --exclude-ns=Sentry\\,Jean85\\ trace.xt

# Changer le préfixe de chemin (défaut: /app/)
legacy-map parse --path-prefix=/var/www/ trace.xt

# Taille du buffer de traces en mémoire (défaut: 20)
legacy-map serve --buffer-size=50 ./xdebug-traces
```

## Pré-requis

- Go 1.22+
- PHP avec XDebug 3.x
- Un projet Symfony (ou tout projet PHP)

## Licence

MIT
