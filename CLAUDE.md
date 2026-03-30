# legacy-map

Outil CLI Go qui capture et analyse les traces XDebug pour cartographier les flux d'exécution d'une application PHP/Symfony.

## Architecture

- `internal/parser/` : parser du format trace_format=1 (TSV streaming, bufio.Scanner)
- `internal/filter/` : filtrage par namespace (trie), exclusion fonctions internes/bootstrap, collapse vendor closures
- `internal/calltree/` : construction CallTree, merge appels répétés, dédup external calls, détection services
- `internal/watcher/` : fsnotify watcher + ring buffer thread-safe (sync.RWMutex)
- `internal/mcp/` : MCP server (stdio) exposant les traces parsées via 4 tools

## Commandes

- `legacy-map init` : setup XDebug + dossier traces pour un projet PHP/Symfony
- `legacy-map parse <file.xt>` : parse une trace et affiche le JSON sur stdout (stats sur stderr)
- `legacy-map watch <dir>` : surveille et parse en continu
- `legacy-map serve <dir>` : watcher + MCP server (stdio)
- `legacy-map setup-mcp [dir]` : configure Claude Code pour utiliser legacy-map comme MCP server

## Flags communs

- `--exclude-ns` : namespaces supplémentaires à exclure (ex: `Sentry\,Jean85\`)
- `--app-ns` : préfixes namespace applicatif (défaut: `App\`)
- `--path-prefix` : préfixe à retirer des chemins (défaut: `/app/`)
- `--buffer-size` : nombre de traces en mémoire (défaut: 20, pour watch/serve)
- `--http-timeout` : timeout HTTP par défaut pour trigger_trace en secondes (défaut: 30, pour serve)

## Conventions

- Plan first, wait for explicit approval before coding
- Tests unitaires pour le parser et le filtre (composants critiques)
- Le parser doit streamer le fichier — ne jamais charger un .xt en mémoire
- Les TraceResult sont stockés dans un ring buffer thread-safe (sync.RWMutex)
- Les valeurs de params/return sont tronquées à 200 chars pour limiter le bruit
- Les appels répétés consécutifs sont mergés (call_count)
- Les external calls sont dédupliqués par nœud

## MCP Tools

- `trigger_trace(url, method, body, headers, timeout)` : déclenche une requête HTTP avec XDebug tracing, attend la trace, retourne le call tree
- `get_last_trace(n)` : N dernières traces avec call tree complet
- `get_trace_by_uri(uri, limit)` : recherche partielle par URI (limit défaut: 1)
- `list_traces()` : métadonnées uniquement (pas de call tree)

## Build & Test

```bash
make build    # ou: go build -o legacy-map .
make test     # ou: go test ./...
make install  # go install avec version injectée
```
