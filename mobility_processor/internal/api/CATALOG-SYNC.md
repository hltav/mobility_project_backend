# Catálogo no Processor principal

Nenhuma sincronização, teste, build ou consulta real foi executada pelo Codex.

O endpoint existente `POST /admin/sync/full` já persistia linhas, mas disparava
goroutine sem cancelamento, dez workers fixos, busca adicional de paradas e
escritas no Valkey. Retornava apenas started, sem quantidades e sem proteção
administrativa no código. Não era adequado para uma primeira carga controlada.

Agora esse endpoint sincroniza somente o catálogo PostgreSQL, de forma síncrona.
Não cria outro sincronizador: usa SyncLinesContext (extensão contextual do mesmo
SyncLines), sessão/cookie jar SPTrans existentes e UpsertFromSPTrans, com o mesmo
ON CONFLICT por provider/CL. Não inicia cargas no startup nem atualiza paradas,
GPS ou Valkey. Os endpoints específicos de paradas/cache continuam separados.

## Autorização e limites

Não havia autenticação administrativa no handler existente. Foi acrescentado
Bearer token exclusivo desta ação, lido de PROCESSOR_ADMIN_TOKEN (mínimo 32 bytes).
Ausente/curto: 503 sem trabalho. Token errado/ausente: 401. Nunca reutilizar o
token da SPTrans. O guard protege esta ação no Go, inclusive chamada direta;
não representa proteção dos demais endpoints administrativos legados.

Body obrigatório: terms com 1–10 termos, cada um não vazio e até 100 bytes.
Termos repetidos são removidos. workers default 1, permitido 1–2;
timeout_seconds default 60, permitido 1–300. JSON até 4096 bytes, sem campos
desconhecidos. Não há carga completa implícita quando terms é omitido.

Uma carga completa por Handler/Processor; outra retorna 409. Não é lock entre
réplicas nem coordena os outros endpoints de sincronização legados. O request
aguarda os workers e retorna a contagem final. Cancelamento do cliente, deadline
e SIGINT/SIGTERM cancelam a coleta; não fica goroutine de trabalho em background.
A contagem final tem orçamento adicional de até 2 s. Upserts já concluídos não
são revertidos por falhas posteriores; reexecução mantém a identidade CL.

Exemplo de resposta sintética:

```json
{"status":"completed","terms":["8000"],"workers":1,"received":4,"upserts":4,"error_count":0,"total_postgresql":4,"count_available":true}
```

received conta registros decodificados das respostas, inclusive CL repetido em
termos distintos; upserts conta operações bem-sucedidas, não apenas inserções.
total_postgresql é COUNT(*) de public.lines, não a quantidade distinta de códigos.
Status pode ser completed, partial, timeout ou canceled. Falhas parciais/deadline
retornam JSON HTTP 200 com status explícito; erro da contagem produz total null.
Erros de validação/auth/busy/dependências usam 400/401/409/503, antes da coleta.
Logs só incluem status e quantidades, sem token ou corpo de resposta da SPTrans.

## Execução manual dentro do serviço principal

Na raiz do repositório, preparar um token administrativo próprio, sem imprimi-lo:

```bash
read -rs -p 'Token administrativo (mínimo 32 caracteres): ' PROCESSOR_ADMIN_TOKEN
printf '\n'
export PROCESSOR_ADMIN_TOKEN

compose=(docker compose -f docker-compose.yml -f docker-compose.infra.yml -f docker-compose.gtfs-local.yml)
# Se este override já estiver em uso, preservá-lo:
# compose+=(-f docker-compose.predictions-local.yml)
compose+=(-f docker-compose.catalog-local.yml)

# Somente o Processor principal; nenhum container auxiliar ou binário separado.
"${compose[@]}" build processor
"${compose[@]}" up -d --no-deps processor

"${compose[@]}" exec -T processor sh -c '
  wget -T 330 -qO- \
    --header="Authorization: Bearer $PROCESSOR_ADMIN_TOKEN" \
    --header="Content-Type: application/json" \
    --post-data='"'"'{"terms":["8000"],"workers":1,"timeout_seconds":30}'"'"' \
    "http://127.0.0.1:${PORT:-8080}/admin/sync/full"
'
```

Essa chamada consulta SPTrans e grava linhas reais: executar deliberadamente e
acompanhar o JSON retornado. Não chamar a partir de polling do frontend.
Depois da primeira carga, uma varredura explícita pode usar termos 0–9, workers 1
ou 2 e timeout até 300 s, alterando somente o JSON acima. Se houver timeout,
dividir em grupos menores. A busca por dígitos não é prova de cobertura integral
do fornecedor; não remove linhas antigas nem marca circulação/inatividade.

Para acompanhar logs (a conclusão também é devolvida pela própria requisição):

```bash
"${compose[@]}" logs --tail=50 processor
```

Verificação somente leitura no PostgreSQL, com o mesmo Compose configurado:

```bash
"${compose[@]}" exec -T postgres psql -X -U open_transit -d open_transit -P pager=off -v ON_ERROR_STOP=1 <<'SQL'
BEGIN READ ONLY;
SELECT count(*) AS registros, count(DISTINCT sptrans_line_id) AS cls,
       count(DISTINCT display_code) AS codigos
FROM public.lines WHERE provider='sptrans';
SELECT id,display_code,sptrans_line_id,sptrans_direction
FROM public.lines WHERE provider='sptrans' ORDER BY display_code,id LIMIT 30;
ROLLBACK;
SQL
```

O catálogo fica disponível no Laravel `GET /api/lines`, autenticado e paginado.
Não exige GTFS ou ônibus circulando. Preservar todos os overrides já usados no
deploy, especialmente o de previsões, ao reconstruir/recriar o Processor.

## Testes preparados, não executados

```bash
cd mobility_processor
go test ./internal/api ./internal/sptrans ./internal/lines ./internal/predictions -count=1 -v
go test -race ./internal/api ./internal/sptrans -count=1
```

Fixtures sintéticas/mocks cobrem termo único, CL/SL, reexecução em store simulado,
autorização, parâmetros, conflito simultâneo, erro parcial, cancelamento,
deadline, concorrência do cliente e término dos workers. Testes existentes de
GPS e previsão continuam incluídos na validação manual. Reexecução PostgreSQL
real e comportamento integrado do container ainda precisam ser validados.
O limite de duas buscas reduz concorrência, mas a quota externa e recursos são
compartilhados com GPS/ETA: executar a primeira carga com um worker.
