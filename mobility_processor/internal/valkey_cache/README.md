════════════════════════════════════════════════════════════════════════════════
 CACHE VALKEY — DOCUMENTATION INDEX
════════════════════════════════════════════════════════════════════════════════

📂 mobility_processor/internal/cache/

Este diretório contém a camada de cache (Valkey/Redis file-based).
Arquitetura: Cache-aside pattern com TTLs automáticas e fallback gracioso.

════════════════════════════════════════════════════════════════════════════════
📄 ARQUIVOS
════════════════════════════════════════════════════════════════════════════════

1. store.go (95 linhas)
   Implementação core do cache (Get/Set/Delete)
   - InitFromEnv(): inicializa store na startup
   - GetJSON(key, dst): tenta ler, retorna (hit bool, err error)
   - SetJSON(key, v, ttl): escreve com TTL automática
   - Delete(key): remove chave
   - Usa SHA1 para hashing de chaves
   - Armazenamento em arquivos JSON com expires_at

   📌 Usar: Quase nunca direto. Use cache.GetJSON/SetJSON nos handlers.

───

1. ttl.go
   Constantes de TTL para cada tipo de dado:
   - TTLLines = 60 *24* time.Hour (linhas, dados quasi-imutáveis)
   - TTLStops = 30 *24* time.Hour (paradas)
   - TTLBuses = 60 * time.Second (ônibus, tempo real)
   - TTLGeocode = 30 *24* time.Hour (endereços)

   📌 Referência ao escrever handlers: use cache.TTL*

───

1. schema.go (270+ linhas) ⭐ REFERÊNCIA PRINCIPAL
   Define tipos Go e funções de geração de chaves:

   TIPOS:
     - CachedLine, CachedStop, CachedGeocodeResult, CachedNearbyLine,
       CachedBusPosition, CacheMetadata

   FUNÇÕES DE CHAVE (SEMPRE USE ESTAS):
     - KeyLineByID(sptransID) → "lines:id:33624"
     - KeyLineByCode(code, dir) → "lines:code:8000-10:dir:0"
     - KeyStopByCP(sptransCode) → "stops:cp:1023"
     - KeyStopsForLine(lineID) → "stops:line:1"
     - KeyGeocodeAddress(address) → "geocode:addr:<hash>"
     - KeyNearbyLines(lat, lng, radius) → "nearby:lines:-23.5614:-46.6558:1.00"
     - KeyNearbyStops(lat, lng, radius) → "stops:nearby:<truncated>"
     - E mais...

   📌 Começo aqui: precisa adicionar cache a um endpoint?
       1. Veja CachedXxx relevante
       2. Use cache.KeyXxx(...) para gerar chave
       3. Siga pattern de INTEGRATION_GUIDE.txt

───

1. CACHE_DESIGN.txt ⭐ ESTRATÉGIA E FLUXOS
   Explicação completa:
   - Hierarquia de prefixos de chaves
   - Fluxos de leitura (cache-aside)
   - Fluxos de escrita (sync com background population)
   - TTLs e rationale
   - Invalidação e warming
   - Fallback e robustez
   - Exemplo prático end-to-end

   📌 Entender por quê cacheamos isto e não aquilo? Leia isto.

───

1. INTEGRATION_GUIDE.txt ⭐ PADRÕES PRÁTICOS
   Como adicionar cache a handlers:
   - Padrão de handler READ (cache-aside)
   - Padrão de handler WRITE (sync + background)
   - Checklist antes de merge
   - Matriz: quais endpoints cacheiam?
   - Guia passo a passo para novo endpoint
   - Erros comuns e soluções

   📌 Precisa escrever código? Siga os padrões aqui.

───

1. PAYLOADS.md ⭐ EXEMPLOS REAIS
   Exemplos JSON de cada tipo de dado:
   - Chave exata
   - Tipo Go
   - Quem escreve/quem lê
   - JSON formatado com comentários
   - Notas sobre armazenamento em disco
   - Tamanho estimado e total

   📌 Quer saber qual é o formato JSON de lines:id:*? Procure aqui.

════════════════════════════════════════════════════════════════════════════════
🚀 QUICK START (30 SEGUNDOS)
════════════════════════════════════════════════════════════════════════════════

Adicionar cache a um novo endpoint:

1. Vá a schema.go
   - Defina type CachedXxx struct
   - Defina func KeyXxx(...) string
   - Copie exemplo de outro tipo se incerto

2. Vá a seu handler (ex: handler.go)
   - Cole padrão READ ou WRITE de INTEGRATION_GUIDE.txt
   - Use cache.KeyXxx(...) para gerar chave
   - Use cache.GetJSON/SetJSON com tipos de schema.go

3. Test:
   - Sem cache: primeiro hit vai ao DB
   - Com cache: segundo hit vem do cache (~50ms)
   - TTL: esperar TTL passar, miss novamente

4. Deploy:
   - Checklists de INTEGRATION_GUIDE.txt
   - Push e merge

════════════════════════════════════════════════════════════════════════════════
❓ FAQ RÁPIDO
════════════════════════════════════════════════════════════════════════════════

P: Por que cache é file-based e não Valkey real?
R: Minimal setup sem dependência externa. Migrar para Redis depois é trivial
   (mesmo interface).

P: E se cache ficar indisponível?
R: Fallback automático. GetJSON retorna (false, nil), handler lê DB. Sem crash.

P: Qual TTL escolher?
R: Veja CACHE_DESIGN.txt seção 4. Mínimo 1h para queries pesadas,
   30d+ para dados quasi-imutáveis.

P: Por que background population (goroutines)?
R: Não bloqueia resposta HTTP. DB commit é garantido, cache é best-effort.

P: Como invalidar cache?
R: TTLs expiram naturalmente. Forçada: delete arquivo ou incrementar
   meta:cache:version.

P: Posso compartilhar cache entre Laravel e Go?
R: Sim, futuro. Laravel acessa Redis, Go escreve. Requer sync de tipos.

P: Como debugar cache?
R: Verificar arquivos em /tmp/valkey/. Logs em handler (se hit/miss).
   Adicionar healthcheck endpoint futuro.

════════════════════════════════════════════════════════════════════════════════
📋 CHECKLIST: ANTES DE ADICIONAR CACHE A NOVO ENDPOINT
════════════════════════════════════════════════════════════════════════════════

[ ] Dados são quasi-imutáveis ou reutilizáveis? (Se não, pule cache)
[ ] Defini CachedXxx em schema.go com exemplo JSON?
[ ] Defini funções KeyXxx em schema.go?
[ ] Usei cache.KeyXxx(...) no handler (NUNCA fmt.Sprintf)?
[ ] GetJSON trata hit/miss corretamente?
[ ] SetJSON é background (goroutine) em handlers WRITE?
[ ] Adicionei UpdatedAt ao payload?
[ ] TTL escolhido apropriado?
[ ] Trato erros de cache silenciosamente?
[ ] DB commit é SEMPRE antes de cache?
[ ] Endpoint funciona sem cache (fallback)?
[ ] Testei hit/miss?
[ ] Documentei em PAYLOADS.md?

════════════════════════════════════════════════════════════════════════════════
🔗 MAPA DE REFERÊNCIA: QUAL ARQUIVO LER
════════════════════════════════════════════════════════════════════════════════

Pergunta                              Arquivo a Ler
───────────────────────────────────────────────────────────────────────────────
Preciso adicionar cache a um handler  → INTEGRATION_GUIDE.txt
Quero entender a estratégia           → CACHE_DESIGN.txt
Qual é o formato JSON de ...?         → PAYLOADS.md
Qual função de chave usar?            → schema.go (topo do arquivo)
Como inicializar na startup?          → Ver cmd/server/main.go
Como testar cache?                    → INTEGRATION_GUIDE.txt (seção teste)
Como debugar?                         → ls /tmp/valkey/ (ver arquivos)
Qual TTL escolher?                    → CACHE_DESIGN.txt seção 4
Endpoints cacheados atualmente        → INTEGRATION_GUIDE.txt (matriz)

════════════════════════════════════════════════════════════════════════════════
✅ STATUS ATUAL (2026-06-03)
════════════════════════════════════════════════════════════════════════════════

✓ store.go: Implementado e testado
✓ schema.go: Tipos e funções de chave definidos
✓ Integrado em 5 handlers: Geocode, ResolveLine, NearbySpatial, Sync, SyncStops
✓ Documentação completa (CACHE_DESIGN, INTEGRATION_GUIDE, PAYLOADS)

⚠ TODO:

- Adicionar NearbyStops handler (uso de stops:nearby:*e stops:line:*)
- Adicionar BusPositions handler se necessário (volátil, TTL=60s)
- Migrar para Redis real (atuais: file-based Valkey)
- Healthcheck endpoint para status do cache
- Métricas: hits/misses por endpoint
- Integração com Laravel (compartilhar cache via Redis)

════════════════════════════════════════════════════════════════════════════════
