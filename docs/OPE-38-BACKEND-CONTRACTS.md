# OPE-38 — contratos de percursos e paradas

## Diagnóstico e escopo

Auditoria estática, sem consultas ao PostgreSQL/Valkey e sem executar testes. Nenhum AGENTS.md foi encontrado nos ancestrais ou nos diretórios examinados do projeto. Alterações locais preexistentes foram preservadas, incluindo a remoção anterior de código comentado de LineService.php. Não houve acesso a merged.yml, serviços remotos, execução de migrations/importações, inicialização de serviços ou commit.

O fallback anterior de `LineService::stopsForLine()` resolvia outro CL SPTrans, mas continuava lendo `$line->busStops()` do ID interno original. Isso explica a possibilidade de repetir a mesma lista para direction=1/2. O relacionamento `bus_stop_line` não contém sequência GTFS. O GET ainda chamava `ProcessorService::syncStopsForLine()`, que faz POST `/sync/stops`: esse handler Go consulta SPTrans, grava paradas/relações PostgreSQL e atualiza Valkey em background.

O Laravel lia `stops:line:<CL>:dir:<direction>`; o handler Go grava `stops:line:<CL>`, com payload SPTrans bruto. Portanto, o sync não preenche a chave esperada pelo Laravel. O comentário sobre importgtfslinestops em ValkeyService está desatualizado: hoje esse comando reutiliza o pipeline PostgreSQL canônico. O resolver Go já consulta PostgreSQL para evitar direções inferidas em caches antigos; o Laravel ainda priorizava esse cache.

As aproximadamente 14 paradas podem vir da chave antiga ou da associação legada, sem qualquer vínculo obrigatório ao pattern. A origem exata da resposta observada depende do antigo campo `data.source`; a quantidade e o conteúdo não foram confirmados em runtime. Não foi localizado endpoint de geometria equivalente nas rotas Laravel/Go examinadas. Não se atribuiu o problema ao Angular.

## Identidades e compatibilidade

`/api/lines`, busca Go e favoritos expõem IDs internos de `public.lines`; `sptrans_id` é um identificador distinto. `/api/lines/{id}` e `/api/lines/{lineId}/buses` recebem ID interno e só então resolvem CL. O DTO de detalhe existente omite o próprio id em `LineDTO::toArray()`; esse comportamento preexistente foi preservado. O frontend separado não foi examinado nem alterado.

Login, busca, favoritos e ônibus não tiveram implementação alterada. O endpoint legado permanece autenticado, com envelope `message/data`, `data.line`, `data.stops` e os campos habituais das paradas. Agora consulta apenas o banco local, resolve outro sentido para um registro operacional único e não dispara sync nem lê caches sem procedência. Acrescenta `resolved_line_id`, `official=false`, `ordered=false`, `status=legacy_unordered`. Se não encontrar sentido único, devolve lista vazia, `direction_unavailable` e identificadores resolvidos nulos. Seu `line.id` continua sendo o solicitado para compatibilidade.

## Endpoints novos, sob auth:sanctum

### GET /api/v1/lines/{id}/journey-patterns

Usa exatamente o ID operacional indicado. Consulte os dois IDs internos para os dois sentidos. `direction` e `feed_version_id` não vazios são rejeitados com 422: não há seleção implícita de outro CL ou feed nesse contrato.

Reutiliza `gtfs.active_*`, `ingest.feed_publications` e `public.line_route_mappings`. Exige mapping `confirmed`, pattern explícito, direção GTFS coincidente inclusive para nulos, e janela de validade vigente. Matching apenas de rota não seleciona pattern arbitrário. Todas as junções GTFS incluem `feed_version_id`. Múltiplas variantes continuam separadas pelo pattern, sem escolher uma como principal.

Exemplo esperado diante do estado informado para a linha 8000-1 (não coletado do servidor):

```json
{"data":{"contract_version":1,"line":{"id":1,"code":"8000-1","sptrans_line_id":2506,"sptrans_direction":1},"status":"unavailable","reason":"no_active_confirmed_pattern","patterns":[]}}
```

Linha inexistente: 404. Sem autenticação: 401. Pattern disponível com shape ausente/inválida conserva paradas e informa geometry_status; geometry/bbox ficam nulos. O status geral indica existência de patterns, não garante geometria para todos.

### GET /api/v1/gtfs/preview/feeds/{feedId}/routes/{routeId}/journey-patterns

Prévia explicitamente separada, exige `GTFS_PREVIEW_ENABLED=true`, ambiente Laravel `local` ou `testing`, autenticação e permissões SELECT já existentes nas tabelas base. Default: desabilitada (404). O papel restrito mobility_app recebe 503 quando habilitada, pois não tem esses grants. Nenhuma permissão ou conexão de importador é criada/reutilizada automaticamente. Não ampliar o papel de produção para habilitar a prévia.

Consulta apenas a rota indicada dentro de um feed `ready`; staging/failed, feed ou rota inexistentes retornam `unavailable`, `ready_feed_route_not_found`, lista vazia. Não publica o feed, não gera matching e não afirma associação operacional, mesmo se houver semelhança de código.

Exemplo **fictício**, com geometria e coordenadas ilustrativas; os arrays reais contêm todos os pontos/paradas:

```json
{
  "data": {
    "contract_version": 1,
    "mode": "preview",
    "operational_line_id": null,
    "sptrans_direction": null,
    "feed_version_id": 90,
    "route_id": 900,
    "status": "available",
    "reason": null,
    "patterns": [{
      "route_id": 900,
      "gtfs_route_id": "fixture-route",
      "pattern_id": 901,
      "variant_id": 901,
      "gtfs_direction_id": 0,
      "headsign": null,
      "origin": {"id": 10, "gtfs_stop_id": "A", "name": "Origem", "lat": -23.55, "lng": -46.63, "pattern_sequence": 0},
      "destination": {"id": 11, "gtfs_stop_id": "B", "name": "Destino", "lat": -23.56, "lng": -46.65, "pattern_sequence": 20},
      "shape_id": 902,
      "gtfs_shape_id": "fixture-shape",
      "geometry_status": "valid",
      "geometry": {"type": "LineString", "coordinates": [[-46.63,-23.55],[-46.64,-23.54],[-46.65,-23.56]]},
      "bbox": [-46.65,-23.56,-46.63,-23.54],
      "source": "gtfs",
      "feed_source_id": 9,
      "feed_version_id": 90,
      "publication_status": "preview",
      "matching_status": "unverified",
      "official": false,
      "stops": [
        {"id": 10, "gtfs_stop_id": "A", "name": "Origem", "lat": -23.55, "lng": -46.63, "pattern_sequence": 0},
        {"id": 11, "gtfs_stop_id": "B", "name": "Destino", "lat": -23.56, "lng": -46.65, "pattern_sequence": 20}
      ]
    }]
  }
}
```

No contrato operacional, cada pattern tem os mesmos campos, mas `publication_status=active`, `matching_status=confirmed`, `official=true`; o vínculo operacional fica em `data.line`. Identificadores de pattern/variante são escopados ao feed. `origin` e `destination` são a primeira/última ocorrência oficial de parada, não terminais SPTrans presumidos; headsign pode ser nulo. A geometria é exclusivamente `gtfs.shapes.path_geom` com estado valid, sem simplificação ou reconstrução por GPS/paradas. GeoJSON usa `[longitude, latitude]`; bbox usa `[west,south,east,north]`. Leaflet pode enquadrar com `[[south,west],[north,east]]`. Paradas vêm de journey_pattern_stops, ordenadas por pattern_sequence; ocorrências repetidas são preservadas e sequências podem ter lacunas.

## Matching e publicação — referência 8000-1

Dados fornecidos na tarefa, não reconsultados: feed 1 ready, nenhuma publicação ativa, mappings vazios. Operacionais: id=1/CL=2506/SL=1 e id=2/CL=35274/SL=2. GTFS: rota 971; pattern 1655/direction_id=0/shape informado 70234/19 paradas/170 pontos/Terminal Lapa → Centro; pattern 1656/direction_id=1/shape informado 70235/17 paradas/197 pontos/Centro → Terminal Lapa. O contrato distingue a PK shape_id do identificador textual gtfs_shape_id.

As distâncias e a progressão fornecidas sustentam a consistência interna entre paradas e shapes GTFS. Não comprovam se CL=2506 corresponde ao pattern 1655 ou 1656. Faltam evidências operacionais de orientação/terminais ou associação oficial por CL. Não se aplicou `SL-1`, inferência por código/destino/proximidade, nem regra fixa desses IDs. O matcher Go existente gera sugestões pending mesmo com score 1 por código exato; não foi modificado ou executado. Nenhum mapping foi gravado.

Assim, a interface poderá inspecionar os dois percursos na prévia, mas o acesso operacional oficial permanecerá indisponível até publicação explícita e matching comprovado/confirmado fora desta entrega. Adoção do contrato pelo Angular permanece pendente e fora de escopo.

## Validação manual — não executada pelo Codex

Com o terminal já configurado para um banco **isolado** open_transit_test/open_transit_schema_test e o schema instalado, sem provisionar nada sobre o banco real:

```bash
cd mobility_backend
POSTGRES_STAGE1_TESTS=1 php vendor/bin/phpunit --configuration phpunit.postgresql.xml --filter JourneyPatternContractTest
```

Os testes usam fixtures transacionais e rollback, sem migrations/importações. Cobrem shape original/curva, bbox, ordem com lacunas/repetições, separação de sentidos e feed, ready sem publicação, grants mobility_app, pending/inconsistência/expiração, gating e autenticação da prévia, e GET legado sem sync/cache. Exigem executor com permissões de fixtures e SET ROLE; o perfil stage1 é intencionalmente sintético, não tenta validar/reimportar o dataset real stage3.

Com API **já em execução**, API_BASE e TOKEN definidos pelo operador (não imprimir token), executar individualmente:

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" "$API_BASE/api/lines/1/stops?direction=1"
curl -fsS -H "Authorization: Bearer $TOKEN" "$API_BASE/api/lines/1/stops?direction=2"
curl -fsS -H "Authorization: Bearer $TOKEN" "$API_BASE/api/v1/lines/1/journey-patterns"
curl -fsS -H "Authorization: Bearer $TOKEN" "$API_BASE/api/v1/lines/2/journey-patterns"
curl -fsS -H "Authorization: Bearer $TOKEN" "$API_BASE/api/v1/gtfs/preview/feeds/1/routes/971/journey-patterns"
```

O último comando requer a habilitação local descrita acima, feita pelo operador no ambiente já utilizado pela API. Deve mostrar dois patterns separados, com 19 e 17 paradas respectivamente, se o estado permanecer como informado. Não se fornecem comandos de publicação/confirmação: dependem da validação operacional pendente. Para regressão, executar manualmente os testes existentes HttpCompatibilityTest e CanonicalSchemaTest no mesmo executor isolado.

Revisão realizada: leitura estática dos arquivos e `git diff --check`, sem executar testes, benchmarks ou validações de runtime. Nenhum resultado de teste é afirmado nesta entrega.
