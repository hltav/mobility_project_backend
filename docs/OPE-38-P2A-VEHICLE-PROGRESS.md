# OPE-38 P2A — GPS e progresso GTFS

## Diagnóstico e implementação

O Processor lê /Posicao/Linha por CL. O payload SPTrans fornece p (prefixo), a (acessibilidade), ta (captura), py (latitude), px (longitude). BusPosition expõe prefix/accessible/captured_at/latitude/longitude. O DTO Laravel legado preserva prefixo/coordenadas, mas descarta captured_at. Não foi encontrado histórico recente de posições ou cálculo de progresso no fluxo examinado; nenhum histórico é criado nesta entrega.

As posições e o GTFS existiam como fluxos separados, sem projeção ou associação operacional confirmada. Isso explica a ausência de próxima parada. O endpoint legado de ônibus permanece com seu contrato original. Quando já consulta a SPTrans, o Processor agora armazena a mesma observação na chave prevista buses:line:<CL>, como CachedBusPosition (lat/lng/captured_at), TTL existente de 60 s. Não substitui ta pelo horário da requisição. O novo fluxo lê apenas esse snapshot via ValkeyService; não chama Processor/SPTrans nem sincroniza dados. Se não houver snapshot recente, retorna unavailable. A entrega não executou consultas de rede nem gravou caches.

Foram reutilizados JourneyPatternRepository, LineRepository, ValkeyService, as views gtfs.active_* e os contratos autenticados. O cálculo espacial está em VehicleProgressRepository; a interpretação de confiança/tempo, em VehicleProgressService. Não há novas tabelas ou migrations.

## Endpoints novos

- GET /api/v1/lines/{id}/vehicles/stop-progress: exige auth:sanctum; usa somente patterns de matching confirmed vigente e publicação ativa, conforme JourneyPatternRepository. Rejeita seleção operacional por query direction/pattern_id/feed_version_id. Múltiplos patterns confirmados devolvem ambiguous sem escolher uma variante. O matching da 8000-1 continua pendente: não há associação definitiva criada nesta entrega.
- GET /api/v1/gtfs/preview/feeds/{feedId}/patterns/{patternId}/lines/{id}/vehicles/stop-progress: exige autenticação, GTFS_PREVIEW_ENABLED, APP_ENV local/testing e permissões já existentes nas tabelas base. Usa apenas feed ready e pattern explicitamente indicado. Um resultado utilizável é provisional, mesmo que os números das direções coincidam. Não publica feed ou confirma mapping. O papel mobility_app restrito não é ampliado; sem grants de prévia, recebe 503.

## Projeção e confiança

O SQL recebe um lote de observações e os IDs de patterns selecionados. Usa ST_DumpSegments, ST_LineLocatePoint e ST_LineInterpolatePoint. A interpolação local é sobre a geometria WGS84; todas as medidas reportadas usam ST_Distance/ST_Length com geography, em metros. Nenhum comprimento em graus é reportado como metros. A projeção é uma aproximação por segmento em WGS84, apropriada ao diagnóstico urbano; não é um estimador cinemático de precisão.

progress_fraction é o comprimento geográfico percorrido até a projeção dividido pelo comprimento geográfico total; shape_segment_index é o índice original do segmento, começando em 1. remaining_distance_m é a distância ao longo dos segmentos até a próxima parada candidata, não a distância em linha reta nem a distância até o fim do percurso.

As paradas são projetadas no mesmo shape e ordenadas exclusivamente por pattern_sequence. Não há seleção de outra rota por proximidade. Detectam-se alternativas de projeção cuja distância lateral esteja até 5 m da melhor: se a dispersão longitudinal supera 25 m, a localização é ambiguous. A mesma proteção vale para paradas em loops/sobreposições. Ordem projetada decrescente além de 1 m também é ambiguous. Essas heurísticas conservadoras podem recusar situações utilizáveis e não garantem resolver todos os cruzamentos. Não se usa monotonicidade temporal presumida.

Limites iniciais explícitos em config/gtfs.php: GPS até 120 s; tolerância lateral de veículo 100 m. Horários futuros além de 30 s, ausentes, sem timezone ou inválidos ficam unavailable. GPS antigo fica stale, sem progresso/próxima parada. Paradas projetadas a mais de 100 m do shape ou dados incompletos ficam unavailable. Prefixos duplicados no snapshot ficam ambiguous. Projeções inexistentes conservam os campos desconhecidos nulos.

matched significa associação operacional confirmada com uma projeção espacial utilizável, não passagem ou embarque comprovados. previous_stop e next_stop são vizinhas da projeção na sequência GTFS; stop_status=candidate_only e passage_status=not_observed aparecem sempre. Na projeção sobre uma parada, ela permanece candidata seguinte, com distância zero e tolerância numérica de 1 m. Ao terminar a última parada, next_stop e remaining_distance_m ficam nulos. Não há wrap automático para o início de uma linha circular. Para afirmar passagem efetiva, será necessário histórico temporal validado, identidade CL/prefixo estável e critérios explícitos de progressão, proximidade e histerese.

## JSON real, exemplo sanitizado

Os valores abaixo são fictícios e ilustram um resultado de prévia, sem alegar captura real ou matching da 8000-1:

```json
{
  "data": {
    "contract_version": 1,
    "mode": "preview",
    "source": "processor_snapshot",
    "status": "available",
    "reason": null,
    "vehicles": [{
      "vehicle_prefix": "fixture-01",
      "operational_line_id": 7,
      "sptrans_line_id": 99001,
      "sptrans_direction": 1,
      "observed_at": "2026-11-18T12:00:00+00:00",
      "feed_version_id": 90,
      "pattern_id": 901,
      "gtfs_direction_id": 0,
      "progress_fraction": 0.25,
      "lateral_distance_m": 3,
      "shape_segment_index": 8,
      "projected_position": {"lat": -23.55, "lng": -46.63},
      "previous_stop": {"id": 10, "gtfs_stop_id": "A", "name": "Parada A", "pattern_sequence": 0, "along_m": 100, "lateral_distance_m": 2, "ambiguous": false},
      "next_stop": {"id": 11, "gtfs_stop_id": "B", "name": "Parada B", "pattern_sequence": 10, "along_m": 600, "lateral_distance_m": 2, "ambiguous": false},
      "remaining_distance_m": 350,
      "match_status": "provisional",
      "confidence_reason": "explicit_pattern_unconfirmed_spatial_candidates_only",
      "passage_status": "not_observed",
      "stop_status": "candidate_only"
    }]
  }
}
```

Outros match_status: matched, ambiguous, unavailable, stale. Sem snapshot: data.status=unavailable, reason=vehicle_snapshot_unavailable, vehicles=[]. O status do envelope indica disponibilidade de fontes/patterns; o frontend deve observar match_status de cada veículo. Dados de posição bruta continuam disponíveis no endpoint de ônibus existente. observed_at mantém o horário da observação e nunca representa o horário de leitura do cache.

## Cache, desempenho e limitações de ETA

Uma leitura de snapshot por CL; nenhuma chamada externa por veículo. Uma consulta espacial em lote apenas para observações válidas e patterns selecionados; shapes segmentados dentro da mesma consulta. Sem importação nova, varredura de todas as rotas ou escrita de progresso durante GET. A consulta ainda projeta as paradas por requisição: não foi feito benchmark nem alegação de desempenho validado.

Cache de progresso não foi ativado nesta primeira entrega. Projeto de chave futura: progress:v1:<CL>:<prefix>:<feed>:<pattern>:<observed_at>:<hash-lat-lng>:<publication-generation>, incluindo versão do algoritmo e revisão/validade do mapping. Revalidar associação e frescor antes de ler, TTL nunca superior ao restante da validade GPS. Nova observação/coordenada muda a chave; não reutilizar progresso anterior. Não há histórico ou ETA derivado desse cache.

Sem velocidade observada confiável, histórico, trânsito e tempo de permanência, não há ETA. Não foram inventados velocidade, sentido ou horário de chegada. Feed ready sozinho não habilita matched; continuam pendentes confirmação operacional dos CLs e publicação explícita.

## Arquivos desta entrega

- mobility_processor/internal/api/buses.go e buses_test.go.
- mobility_backend/app/Services/ValkeyService.php e VehicleProgressService.php.
- mobility_backend/app/Repositories/VehicleProgressRepository.php.
- mobility_backend/app/Http/Controllers/VehicleProgressController.php.
- mobility_backend/routes/api.php e config/gtfs.php.
- mobility_backend/tests/Feature/VehicleProgressServiceTest.php.
- mobility_backend/tests/PostgreSQL/JourneyPatternContractTest.php (cenário espacial adicional).
- Este relatório. Alterações locais anteriores preservadas.

## Comandos manuais — não executados

Build e teste Go, sem iniciar serviço:

```bash
cd mobility_processor
go build -o /tmp/open-transit-processor-p2a ./cmd/server
go test ./internal/api
```

Testes Laravel comuns, com dependências já instaladas:

```bash
cd ../mobility_backend
php vendor/bin/phpunit --filter VehicleProgressServiceTest
```

PostGIS, exclusivamente com o executor/banco isolado já configurado e schema instalado:

```bash
POSTGRES_STAGE1_TESTS=1 php vendor/bin/phpunit --configuration phpunit.postgresql.xml --filter JourneyPatternContractTest
```

Com a API já em execução, API_BASE e TOKEN definidos pelo operador, e snapshot preenchido pelas consultas normais de ônibus do frontend:

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" "$API_BASE/api/v1/lines/1/vehicles/stop-progress"
curl -fsS -H "Authorization: Bearer $TOKEN" "$API_BASE/api/v1/gtfs/preview/feeds/1/patterns/1655/lines/1/vehicles/stop-progress"
curl -fsS -H "Authorization: Bearer $TOKEN" "$API_BASE/api/v1/gtfs/preview/feeds/1/patterns/1656/lines/2/vehicles/stop-progress"
```

Sem snapshot ou com observação velha, esperar indisponibilidade/stale; não forçar uma associação para obter next_stop. Os testes preparados cobrem transformação do snapshot sem inventar captura, metros/fração no PostGIS e isolamento feed/pattern, escolha de candidatas, distância longitudinal, dados antigos/sem horário, ambiguidades, geometria fora da tolerância, fim de percurso e proteção dos endpoints. Não foram executados nem se afirma que passaram.

## Para o chat do frontend

Fornecer estes endpoints, campos e estados; explicar que a prévia exige escolha explícita de linha/feed/pattern e deve ser rotulada provisória. Usar prefixo junto ao ID operacional, verificar observed_at e match_status, limpar indicação antiga quando GPS muda/expira e tratar todos os campos nulos. Não trocar o marker GPS bruto pela projeção sem decisão de interface. Identificar previous_stop/next_stop como candidatas espaciais; não mostrar passagem confirmada ou ETA. A confirmação/publicação pendente é requisito para a modalidade operacional matched. Nenhuma alteração Angular foi feita.


## Adição P2D.1 — frações das paradas

`previous_stop.progress_fraction` e `next_stop.progress_fraction` são acrescentados quando há projeção utilizável. Cada fração é `clamp(stop.along_m / total_m, 0, 1)`, usando os mesmos metros geográficos acumulados e o mesmo comprimento total do feed/pattern já empregados na fração do veículo e em upcoming_stops. O cálculo reutiliza a consulta existente e não projeta geometrias novamente. A fração da próxima parada é a mesma de upcoming_stops[0].

Todos os campos anteriores das paradas, IDs textuais, PKs, sequência e distâncias são preservados. Sem parada anterior/próxima, o objeto continua nulo. Dados acumulados ausentes/não finitos ou comprimento total não positivo não geram frações. Paradas repetidas mantêm a sequência e sua fração própria. Nenhum estado de confiança ou evidência temporal é promovido: provisional/candidate_only/not_observed continuam iguais.

Testes preparados para frações, referência de pattern, ocorrências repetidas, início/fim do segmento, limites com objetos nulos, dados inválidos/ausentes e envelope provisório. As verificações existentes de GPS desatualizado foram ampliadas para ausência de frações. Não foram executados testes, benchmarks, builds, HTTP ou serviços; integração aguarda validação manual. Nenhum frontend, endpoint, SQL, migration ou banco foi alterado nesta fase. Sem commit, push ou PR.
