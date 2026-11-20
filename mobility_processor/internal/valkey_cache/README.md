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


## OPE-38 P3B.5 — integração com scripts Lua reais

Esta seção descreve o cliente Valkey atual (Go Redis / DB 0). As descrições
históricas de cache em arquivos acima não se aplicam a estes testes.

`prediction_lua_integration_test.go` executa os scripts de produção por meio de
`Client`, `PredictionCache.Save/Read`, `AcquirePredictionLease` e
`FinishPredictionLease`. O hook de teste apenas prefixa as chaves e encaminha os
comandos ao Valkey real; não simula os resultados do Lua. Não instancia o coletor
nem o cliente HTTP SPTrans. Os dados de previsão são sintéticos.

### Proteções e configuração obrigatória

- Sem as três variáveis abaixo, os testes usam `t.Skip` antes de conectar.
- `VALKEY_LUA_TEST_ADDR`: IP de loopback explícito e porta diferente de 6379.
  Não há fallback para localhost, valkey ou variáveis VALKEY_* operacionais.
- `VALKEY_LUA_TEST_ISOLATED=1`: confirmação explícita do ambiente descartável.
- `VALKEY_LUA_TEST_RUN_ID`: identificador exclusivo, de 16–80 caracteres
  alfanuméricos, hífen ou underscore. UUID atende ao formato.
- Antes de qualquer escrita, exige que `test:ope38:lua:instance` contenha o mesmo
  ID na instância isolada. Um endereço/marcador inválido resulta em falha segura.
- Cada teste usa `test:ope38:lua:<run-id>:<aleatório>:` em todas as suas chaves.
  Somente as chaves registradas desse teste são removidas por `DEL` no cleanup.
  Não há FLUSHDB, FLUSHALL, SCAN ou KEYS. O marcador é apenas lido pelos testes.
- Estes testes não são apropriados para uma instância operacional, mesmo com
  namespace. O opt-in e o marcador não substituem a escolha correta do container.

### Execução manual no Ubuntu

Nenhum comando abaixo foi executado pelo Codex. Na raiz `mobility_project`,
crie um container novo sem volumes e sem persistência, somente na porta separada:

```bash
export VALKEY_LUA_TEST_RUN_ID="$(cat /proc/sys/kernel/random/uuid)"
export VALKEY_LUA_TEST_ADDR=127.0.0.1:16379
export VALKEY_LUA_TEST_ISOLATED=1
ope38_lua_container="ope38-valkey-lua-${VALKEY_LUA_TEST_RUN_ID}"

docker run --detach --rm --name "$ope38_lua_container" \
  --publish 127.0.0.1:16379:6379 \
  valkey/valkey:7.2-alpine valkey-server --save "" --appendonly no

docker exec "$ope38_lua_container" valkey-cli PING
```

Espere `PONG` (se ainda estiver inicializando, repita apenas o PING). Não remova
outro serviço se a porta estiver ocupada; escolha outra porta de loopback e ajuste
ambos o publish e VALKEY_LUA_TEST_ADDR. Prepare o marcador somente no container
que acabou de criar:

```bash
docker exec "$ope38_lua_container" valkey-cli SET \
  test:ope38:lua:instance "$VALKEY_LUA_TEST_RUN_ID"

cd mobility_processor
go test ./internal/valkey_cache -run '^TestPredictionLua' -count=1 -v
```

A suíte deve executar, e não apenas mostrar SKIP. Não redirecione esse endereço
para o Valkey do projeto. As conexões usam DB 0 e não carregam credenciais ou
configuração operacional. A execução requer Go/dependências já disponíveis; não
inicia serviços por conta própria.

Ao terminar, mesmo se os testes falharem, pare somente o container criado acima:

```bash
docker stop "$ope38_lua_container"
unset VALKEY_LUA_TEST_ADDR VALKEY_LUA_TEST_ISOLATED VALKEY_LUA_TEST_RUN_ID
```

`--rm` remove o container ao parar; nenhum volume foi associado. Não é necessário
apagar chaves globalmente. Se o processo Go for interrompido abruptamente, a
remoção do container elimina o ambiente descartável.

### Cenários e limites

- Escritas concorrentes de snapshots e preservação da tentativa mais recente;
  rejeição de tentativa antiga/igual sem renovar a retenção física.
- Concorrência de leases entre dois clientes independentes: um único vencedor.
- Token incorreto não libera lease nem modifica o contador de falhas.
- Cooldown impede aquisição e libera após expiração.
- Backoff exponencial, aplicação do teto e reset após sucesso.
- Dono antigo de lease expirado não libera o lease do novo proprietário.

As esperas por expiração têm prazo finito; os testes verificam TTLs reais e podem
falhar em ambiente excessivamente pausado/lento. Não são benchmark ou teste de
carga. Não validam cluster, indisponibilidade de rede, SPTrans ou scheduler.
Não foi identificado defeito comprovado nos scripts por esta revisão estática;
nenhum script de produção foi alterado. A execução manual ainda é necessária.
