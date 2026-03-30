# legacy-map

Outil CLI qui capture les traces XDebug d'une application PHP/Symfony et les transforme en call tree applicatif exploitable par un LLM via MCP.

**Le problème** : une requête Symfony génère des centaines de milliers d'appels de fonctions. Impossible de comprendre ce qui se passe sans filtrer le bruit (vendor, fonctions PHP internes, bootstrap).

**La solution** : `legacy-map` parse les traces XDebug en streaming, filtre agressivement (>99.99% de réduction), et expose un call tree propre via un serveur MCP.

## Résultats typiques

| Trace | Raw calls | Après filtrage | Réduction |
|-------|-----------|----------------|-----------|
| `POST /api/admin/login/check` (146 Mo) | 316,114 | **13 nœuds** | 99.996% |
| `POST /api/auth/admin/login` (45 Mo) | 102,272 | **8 nœuds** | 99.992% |

## Installation

```bash
go build -o legacy-map .
```

## Configuration XDebug

Dans `php.ini` ou `xdebug.ini` :

```ini
[xdebug]
xdebug.mode = trace
xdebug.start_with_request = trigger
xdebug.trace_format = 1
xdebug.trace_output_dir = /path/to/traces
xdebug.trace_output_name = trace.%t.%R
```

Puis déclencher une trace en ajoutant `?XDEBUG_TRACE=1` à l'URL ou en utilisant l'extension navigateur Xdebug Helper.

## Utilisation

### Parser une trace

```bash
legacy-map parse trace.xt
# JSON sur stdout, stats sur stderr
```

### Surveiller un dossier

```bash
legacy-map watch /path/to/traces
# Parse automatiquement les nouveaux fichiers .xt
```

### MCP Server

```bash
legacy-map serve /path/to/traces
# Lance le watcher + serveur MCP sur stdio
```

#### Configuration Claude Code

Ajouter dans `.claude/settings.json` :

```json
{
  "mcpServers": {
    "legacy-map": {
      "command": "/path/to/legacy-map",
      "args": ["serve", "/path/to/traces"]
    }
  }
}
```

#### Tools MCP disponibles

- **`list_traces()`** — Liste les traces en mémoire (URI, durée, nombre d'appels)
- **`get_last_trace(n)`** — Retourne les N dernières traces avec le call tree complet
- **`get_trace_by_uri(uri)`** — Recherche les traces par fragment d'URI

### Options

```
--exclude-ns    Namespaces supplémentaires à exclure (ex: Sentry\,Jean85\)
--app-ns        Préfixes namespace applicatif (défaut: App\)
--path-prefix   Préfixe à retirer des chemins de fichiers (défaut: /app/)
--buffer-size   Nombre de traces en mémoire (défaut: 20)
```

## Ce que le filtre élimine

1. **Fonctions PHP internes** : `strlen`, `array_map`, `sprintf`, etc.
2. **Namespaces vendor** : `Symfony\`, `Doctrine\`, `Twig\`, `Monolog\`, `Psr\`, `Composer\`, etc.
3. **Bootstrap** : `require`, `include`, closures vendor, `ComposerAutoloaderInit`
4. **Sous-arbres vendor** : quand du code `App\` appelle du vendor, seul le nom de l'appel externe est conservé (pas ses 200 appels internes)
5. **Appels répétés** : 14 appels à `registerBundles` → 1 nœud avec `call_count: 14`
6. **External calls dédupliqués** : 90 appels à `Route->getDefault` → 1 seule entrée
