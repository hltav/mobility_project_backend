# Coleta manual de paradas do lote de 30 códigos

Não executado pelo Codex: coleta, chamada SPTrans, escrita operacional, build,
teste e novo dry-run. A comparação ainda não tem resultados posteriores.

## Reutilização e proveniência

syncsptransstops já grava paradas canônicas e referências CP confirmadas por fonte
SPTrans, além de associações line_stop_sources por CL. Contudo GetAllStops varre
termos amplos, sem se restringir ao lote: não usar esse CLI para esta coleta.
O endpoint existente POST /admin/sync/stops agora aceita lista explícita limitada
e mantém o seletor escalar antigo, ambos com autorização e whitelist obrigatórias.

Não há novo sincronizador/fonte: GetStopsByLineContext estende GetStopsByLine com
contexto e sessão existente; UpsertStopsForLine é o mesmo writer transacional.
O writer grava bus_stops, stop_source_references source_kind=sptrans,
method=sptrans_api, raw_attributes e timestamps; line_stop_sources grava a
identidade canônica da linha, external_line_id=CL, external_stop_id=CP e fonte
sptrans. O sentido é o do registro operacional direcional do CL, validado no banco.
Não cria referência GTFS ou mapping de rota. O sort por CP evita ordem de locks
inconsistente; não persiste sequência de atendimento e não prova direção.

O handler exige PROCESSOR_ADMIN_TOKEN com o mesmo Bearer guard do catálogo e
PROCESSOR_STOPS_ALLOWED_CLS com até 60 CLs únicos. Ausente/inválido deixa a coleta
desabilitada. A whitelist é configuração explícita, não IDs fixos em produção.
Cada request valida todos os CLs contra public.lines antes da primeira chamada
externa, recusando identidade ausente, ambígua ou sem SL 1/2.

Limites: até 60 CLs únicos, workers 1 por padrão e no máximo 2, timeout 1–300 s,
cancelamento do request e exclusão mútua por processo para essa ação. Os workers
são aguardados antes da resposta. Não há scheduler ou coleta no startup. Não há
busca textual/catálogo nem linha fora da lista enviada. Não atualiza Valkey.
Os endpoints legados distintos não são desativados ou coordenados por esse lock.

O JSON retorna status, cls_requested, cls_processed, cls_unprocessed,
stops_returned, stops_persisted, new_legacy_links, cls_without_data, errors e
resultado por CL/sentido. Persisted conta operações/linhas do lote, não CPs únicos
globais. Cada CL usa transação própria; falhas não revertem CLs já concluídos.
Resposta vazia válida é empty; null/corpo incompleto é erro, não ausência oficial.

## Comandos manuais — Ubuntu, container principal

Na raiz, carregar a whitelist pública preparada a partir da seleção anterior e
fornecer o token administrativo existente sem imprimi-lo:

```bash
cd ~/Projects/OPEN_TRANSIT/mobility_project
export PROCESSOR_STOPS_ALLOWED_CLS="$(cat docs/evidence/ope38-stops-batch30-plan/allowed-cls.txt)"
read -rs -p 'Token administrativo existente: ' PROCESSOR_ADMIN_TOKEN
printf '\n'
export PROCESSOR_ADMIN_TOKEN

compose=(docker compose -f docker-compose.yml -f docker-compose.infra.yml -f docker-compose.gtfs-local.yml)
# Preservar os overrides já utilizados no ambiente, por exemplo:
# compose+=(-f docker-compose.predictions-local.yml)
# compose+=(-f docker-compose.catalog-local.yml)
compose+=(-f docker-compose.stops-batch-local.yml)
"${compose[@]}" build processor
"${compose[@]}" up -d --no-deps processor

docker cp docs/evidence/ope38-stops-batch30-plan/request.json mobility_processor:/tmp/ope38-stops-request.json
docker exec mobility_processor sh -c '
 wget -T 330 -qO- \
  --header="Authorization: Bearer $PROCESSOR_ADMIN_TOKEN" \
  --header="Content-Type: application/json" \
  --post-file=/tmp/ope38-stops-request.json \
  "http://127.0.0.1:${PORT:-8080}/admin/sync/stops"
' > /tmp/ope38-stops-result.json
python3 -m json.tool /tmp/ope38-stops-result.json
```

request.json contém exatamente os 60 CLs anteriores, workers=1 e timeout=300.
Essa etapa consulta SPTrans e grava dados reais: é execução manual deliberada.
Timeout pode deixar cls_unprocessed; repetir explicitamente somente os CLs que
faltaram/falharam, sempre dentro da whitelist, ou repetir a carga idempotente.
Não acrescentar termos ou fazer varredura ampla. Conferir errors, empty e os
resultados individuais antes de interpretar qualquer ganho do matcher.

## Repetição do matcher no mesmo lote e comparação

Recriar o Processor remove arquivos temporários anteriores. Preparar novamente
o CLI existente e copiá-lo ao container principal, sem container auxiliar:

```bash
cd ~/Projects/OPEN_TRANSIT/mobility_project/mobility_processor
CGO_ENABLED=0 go build -trimpath -o /tmp/ope38-matchroutes ./cmd/matchroutes
docker cp /tmp/ope38-matchroutes mobility_processor:/tmp/ope38-matchroutes

python3 scripts/compare-stop-batch30.py --run-dry-runs \
  --after ../docs/evidence/ope38-batch30-after-stops
```

O wrapper usa somente matchroutes --dry-run --feed-version 1 --line-id para os
60 IDs do arquivo selection.json anterior, comparando seus pares. Máximo dois
processos, timeout interno de 12 s por dry-run, janela de início de 180 s. Não
usa --confirm-write nem um dry-run irrestrito. Verifica identidades e peers nos
resultados. Gera stdout/stderr por ID, comparison.json e comparison.csv.
Se faltarem dry-runs por timeout, o relatório indica missing_cls, sem inventar
resultado. Para apenas comparar arquivos já gerados, omitir --run-dry-runs.

Os totais distinguem orientation_improved, remained_insufficient,
became_ambiguous, became_contradicted e coverage_improved. Baseline: 98 avaliações
insuficientes; 1027-10 não tinha candidato. Novos/removidos candidatos são
contabilizados separadamente e falhas não são tratadas como melhora.

## Testes preparados (não executados)

```bash
cd ~/Projects/OPEN_TRANSIT/mobility_project/mobility_processor
go test ./internal/api ./internal/sptrans ./internal/stops ./internal/matching -count=1 -v
go test -race ./internal/api ./internal/sptrans -count=1
```

Mocks cobrem autorização, whitelist, duplicação, limites, identidade antes de
qualquer coleta, contagens, erro versus vazio, duas requisições concorrentes e
cancelamento com join dos workers. Transporte sintético verifica endpoint/CL,
CP, timeout e rejeição de null. Testes existentes continuam cobrindo o writer e
matching. Persistência real e melhoria permanecem pendentes de validação manual.
É esperado que pertença CL/CP melhore cobertura, mas sem sequência operacional
oficial o matcher continua sem poder inferir progressão/sentido a partir da ordem
retornada pelo fornecedor. Não alterar tolerâncias para contornar essa ausência.
