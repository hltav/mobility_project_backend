# Diagnóstico GTFS ↔ CP SPTrans

Implementação exclusivamente de leitura/revisão. Não há endpoint, comando de
escrita, confirmação automática nem integração ao Stop Progress. Nenhuma consulta
ou teste foi executado pelo Codex. As constraints abaixo foram verificadas no DDL
versionado; verificar sua instalação real com as consultas adiante.

## Identidade e importação verificadas no código

`gtfs.stops.id` é a PK interna (`gtfs_stop_pk`); `gtfs_stop_id` é texto externo,
preservando zeros e caracteres; `stop_code` é outro campo externo, sem prova de
equivalência com CP. A identidade é vinculada ao `feed_version_id`.

Os importadores GTFS `internal/gtfs/{repository,stream_repository}.go` gravam em
`gtfs.stops`, reutilizando `(feed_version_id,gtfs_stop_id)`, sem criar referências
canônicas. O importador SPTrans `internal/stops/repository.go` cria a parada
canônica, sua referência CP `confirmed`/`sptrans_api` e associações CL/CP obtidas
da fonte em `line_stop_sources`. Isso explica por que importar GTFS não preenche
automaticamente as referências GTFS ausentes informadas no diagnóstico real.

Tabelas reutilizadas e constraints relevantes:

- `public.bus_stops`: identidade canônica; ponto público WGS84 e geography.
- `gtfs.stops`: UNIQUE `(feed_version_id,gtfs_stop_id)`, UNIQUE
  `(feed_version_id,id,gtfs_stop_id)` e FK de parentes no mesmo feed.
- `public.stop_source_references`: FK tripla
  `(feed_version_id,gtfs_stop_pk,external_stop_id)` para
  `(gtfs.stops.feed_version_id,id,gtfs_stop_id)`; UNIQUE parcial
  `(feed_version_id,gtfs_stop_pk)` para GTFS. A referência GTFS aponta para o
  mesmo `stop_id` canônico da referência SPTrans, deixando `sptrans_stop_code`
  NULL; CP permanece somente na referência SPTrans existente.
- Referências SPTrans exigem provider/source `sptrans`, CP positivo,
  `external_stop_id=CP::text` e feed/PK GTFS nulos; índice único provider/CP.
- `line_stop_sources`: identidade de linha canônica e CL/CP externos, estado e
  fonte; UNIQUE NULLS NOT DISTINCT da associação completa. Só associações SPTrans
  confirmadas, com CL e CP textuais exatos e feed NULL, sustentam compatibilidade.
- `line_route_mappings`: FK feed/rota/pattern e estado; a consulta verifica também
  direção GTFS e vigência. Não calcula `sptrans_direction-1`.
- `gtfs.journey_patterns` / `journey_pattern_stops`: joins pelo mesmo feed e pattern;
  ordem por `pattern_sequence`, preservando visitas repetidas à mesma parada.
- `ingest.feed_sources`, `feed_versions`, `feed_publications`: fonte, checksum,
  estado ready e indicador de publicação separados.

## Consulta e classificação

`StopReferenceCandidateRepository::candidates(feedId, patternId, lineId, radiusM=50)`
exige IDs explícitos e raio finito de até 200 m. É uma consulta diagnóstica a feed
ready selecionado: não substitui publicação ou matching operacional. Nenhuma rota
HTTP foi adicionada. Contexto inexistente/inválido retorna array vazio; parada de
um contexto válido sem candidata retorna linha com CP NULL e review unavailable.

A consulta considera somente referências SPTrans confirmadas. Apresenta candidatas
por proximidade `ST_DWithin(geography,geography,raio)` em metros, coincidência
textual de ID/código, ou referência GTFS já existente. Nomes são evidência auxiliar.
50 m é raio de triagem, não limiar de identidade homologado. Não escolhe o ponto
mais próximo nem associa IDs por conversão numérica.

Cada resultado preserva feed, checksum, fonte, pattern, sequência, PK/ID GTFS,
linha, CL/SL, identidade canônica, CP, referência SPTrans e método. O objeto
`evidence` separa distância/identificadores, associação CL/CP, compartilhamento
com outro sentido do mesmo código público, matching do pattern e publicação.
A flag de outro sentido é alerta conservador, não prova de direção do pattern.

O repository acrescenta `review.status` e `review.reason`:

- `candidate`: compatibilidade CL/CP, matching explícito do pattern e proximidade,
  mas identidade física ainda não provada.
- `ambiguous`: múltiplos elegíveis, compartilhamento de sentidos ou conflito com
  referência existente; também provider de referência confirmada inconsistente
  ou matching operacional do pattern ainda não confirmado.
- `rejected`: candidata incompatível com o contexto, fora do raio, estação em vez
  de ponto de embarque, rejeição existente ou conflito com referência confirmada.
  É rejeição diagnóstica; não grava estado no banco.
- `confirmed`: somente relata uma referência GTFS já confirmada para a mesma parada
  canônica e provider do feed. Nunca promove candidatas. Essa confirmação física
  não comprova atendimento pela linha/pattern selecionado: conferir as flags.
- `unavailable`: nenhuma referência SPTrans candidata encontrada.

O schema permite apenas `pending`, `confirmed`, `rejected`: candidate/ambiguous
nunca são inseridos como status. Como há uma única referência por parada/feed,
um conjunto ambíguo deve permanecer no relatório de revisão; não criar múltiplas
referências pending nem nova tabela para contornar a constraint.

## Confirmação futura explícita (não implementada nem autorizada nesta etapa)

1. Guardar o relatório com feed/checksum, pattern, CL/SL, CP e IDs de referência.
   Resolver ambiguidades de plataforma/lado da via e visitas/variantes. Ausência
   de outra candidata no cadastro parcial de 27 referências não prova unicidade
   na rede real.
2. Exigir evidência independente e rastreável de que o ID GTFS e CP representam
   o mesmo ponto físico: crosswalk oficial/documentação verificável ou revisão
   operacional documentada. Nome, proximidade e números iguais não bastam.
   Para uso operacional, comprovar também a associação CL/sentido/pattern; a
   revisão de parada não confirma automaticamente `line_route_mappings`.
3. Após autorização de escrita, usar transação e serialização pela identidade
   `(feed_version_id,gtfs_stop_pk)`. Revalidar FK tripla, provider da fonte,
   referência SPTrans confirmada e evidências. Registrar método `manual_review_v1`,
   ator, instante UTC, justificativa, hashes/referências da evidência e contexto em
   `raw_attributes`; não atribuir confidence=1 artificialmente.
4. Mesma identidade já confirmada para o mesmo canônico: no-op idempotente,
   preservando auditoria. Alvo, fonte ou external ID divergente: conflito para
   revisão, sem upsert que sobrescreva confirmação. Rejeições anteriores exigem
   revisão explícita; não são resetadas silenciosamente. Novo feed exige revisão
   própria, mesmo quando o external ID se repete. Não publicar feeds nesse fluxo.

## SQL manual somente leitura

Usar conexão PostgreSQL já configurada pelo operador, sem expor credenciais.
Na raiz do repositório, os IDs abaixo são apenas o caso de referência 8000-1:

```bash
PGOPTIONS='-c default_transaction_read_only=on' psql -X -v ON_ERROR_STOP=1 \
  -v feed_version_id=1 -v pattern_id=1655 -v line_id=1 -v radius_m=50 \
  -f mobility_backend/database/postgresql/diagnostics/stop_reference_candidates.sql
# Repetir para o outro percurso explicitamente selecionado:
PGOPTIONS='-c default_transaction_read_only=on' psql -X -v ON_ERROR_STOP=1 \
  -v feed_version_id=1 -v pattern_id=1656 -v line_id=2 -v radius_m=50 \
  -f mobility_backend/database/postgresql/diagnostics/stop_reference_candidates.sql
```

O SQL retorna evidências; a classificação review é adicionada pela classe PHP.
As consultas abaixo também são somente leitura:

```sql
SELECT provider,source_kind,status,count(*)
FROM public.stop_source_references GROUP BY 1,2,3 ORDER BY 1,2,3;

SELECT c.conrelid::regclass AS relation,c.conname,pg_get_constraintdef(c.oid)
FROM pg_constraint c WHERE c.conrelid IN
 ('public.stop_source_references'::regclass,'public.line_stop_sources'::regclass,
  'gtfs.stops'::regclass,'public.line_route_mappings'::regclass)
ORDER BY 1,2;

SELECT tablename,indexname,indexdef FROM pg_indexes
WHERE schemaname='public' AND tablename IN ('stop_source_references','line_stop_sources');
```

## Testes preparados, sem execução

```bash
cd mobility_backend
php artisan test --filter=StopReferenceCandidateReviewTest
```

`StopReferenceIdentityTest` exige `POSTGRES_STAGE1_TESTS=1`, driver pgsql e banco
`open_transit_test` ou `open_transit_schema_test` no executor isolado existente.
Usa fixtures sintéticas e rollback para verificar repetição de ID entre feeds e
unicidade/idempotência do registro sem rebaixar confirmação. Não executar no
banco real. O escritor de confirmação ainda não existe; o teste cobre sua base
de constraints, não uma futura implementação completa. Validação espacial do SQL
e integração PDO/PostGIS ainda dependem de execução manual.

## Pendências para ETA

Revisar as candidatas reais, ampliar referências CP quando necessário, obter prova
externa de identidade e confirmar explicitamente referências GTFS. Depois, em
etapa própria, resolver pattern/feed → parada canônica → CP, exigir matching
operacional e publicação, cruzar CL + prefixo textual + CP do snapshot e homologar
horários/fuso/virada do dia. Nenhuma geometria, sequência, endpoint ou contrato do
Stop Progress foi alterado.
