# OPE-38 — adendo de matching de sentidos

## Diagnóstico

O matcher anterior produzia pending por código exato com score 1 para qualquer pattern da rota. Seu score espacial media a distância até qualquer extremo do shape; portanto não determinava orientação. Agora a sugestão mantém essa etapa de descoberta e acrescenta uma avaliação orientada por pattern. O score continua sendo de descoberta, não probabilidade de confirmação.

A leitura direta dos CSVs derivados locais, sem alterações, confirmou:

| GTFS | shape | pontos | paradas | headsign | primeira parada | última parada |
|---|---|---:|---:|---|---|---|
| direction_id 0 | 70234 | 170 | 19 | Pça. Ramos De Azevedo | 480014937 — Terminal Lapa - Plat. 1 | 6714586 — Av. S. Luís |
| direction_id 1 | 70235 | 197 | 17 | Term. Lapa | 6714437 — R. Cel. Xavier De Toledo, 254 | 480014610 — Tendal Da Lapa C/B |

Ambos os shapes têm extremos aproximados em Lapa (-23.520438, -46.699642) e Centro (-23.547591, -46.640715), em ordem inversa. A última parada servida não coincide necessariamente com o extremo do shape. A hipótese CL 2506/SL 1 → pattern 1655/direction_id 0 e CL 35274/SL 2 → pattern 1656/direction_id 1 é consistente com o GTFS fornecido, mas ainda depende de comprovação do lado operacional.

Não foi consultado PostgreSQL nem Valkey. Os CSVs não contêm os CLs operacionais. Portanto esta entrega **não afirma que a hipótese foi comprovada no banco**. Não foi procurada conexão em arquivos de credenciais. O dry-run abaixo prepara a verificação pelo operador usando o ambiente já configurado.

O importador operacional grava TP e TS como rótulos independentes e não preenche automaticamente headsign, origin_stop_id ou destination_stop_id. O campo legado origin_lat pode resultar da geocodificação de TP; não é uma origem orientada comprovada. Por isso esses valores não são usados como prova da direção. TP/TS aparecem no diagnóstico para revisão humana, sem seleção presumida por SL. Campos explícitos headsign e referências de paradas operacionais são usados quando disponíveis; ausentes, a evidência fica insufficient.

## Implementação

- Reaproveita matcher, Repository, SaveMatches e public.line_route_mappings. Sem migrations ou nova tabela.
- Carrega extremos das paradas GTFS ordenando por pattern_sequence, com todas as junções escopadas por feed_version_id.
- Compara início com início e fim com fim; preserva a distinção entre SPTrans SL e GTFS direction_id, inclusive nulos. Não existe SL-1.
- Reutiliza a tolerância espacial de 500 m: avalia proximidade entre extremos do shape e primeira/última parada servida; mede referências operacionais contra as respectivas paradas GTFS. Não transforma proximidade em confirmação.
- supported exige alinhamento interno da geometria e headsign operacional explícito coincidente ou ambas as referências operacionais próximas. Comparação textual é conservadora, ignorando apenas caixa/espaços; não expande abreviações.
- contradicted indica referência operacional distante do extremo de paradas correspondente. insufficient indica ausência de evidências suficientes. Duas ou mais variantes supported são marcadas ambiguous; nenhuma é escolhida por PK, score ou equivalência numérica.
- Todas as avaliações são sugestões pending, incluindo supported. O modo escritor existente passa a persistir matching_method com o estado da evidência; ignora avaliações contradicted, preserva confirmed/rejected e não limpa sugestões antigas. Não foi executado.
- Novo --dry-run usa transação PostgreSQL READ ONLY / REPEATABLE READ. Carrega ambos os lados no mesmo snapshot e nunca chama SaveMatches. --line-id filtra ID interno. --dry-run e --confirm-write são mutuamente exclusivos.
- O relatório identifica estado pending_review ou no_match e inclui CL, SL, feed, rota, pattern, direção GTFS, headsigns, rótulos TP/TS, paradas/extremos e distâncias disponíveis. Não representa o estado persistido de um mapping: status=pending significa proposta para revisão, mesmo se já existir decisão persistida.
- O contrato operacional Laravel permanece exclusivamente baseado em confirmed válido e feed publicado. Sem isso, mantém unavailable / no_active_confirmed_pattern.

## Testes preparados, sem execução

direction_test.go cobre a hipótese 8000-1 com evidências operacionais **sintéticas**, convenção invertida SL=1/GTFS=1, equivalência numérica insuficiente, variantes ambíguas, ausência de correspondência, geometria ausente/desalinhada, conflito de terminais, headsign que não confirma e preservação de feed/pattern/direções nulas.

JourneyPatternContractTest.php recebeu cenário de associações confirmadas separadas para os dois sentidos, preservação do feed/pattern e convenção diferente de SL-1. Os demais cenários de pending/expiração/inconsistência/publicação já preparados permanecem. O teste de integração Go existente continua cobrindo SaveMatches, isolamento de feed e preservação de confirmed.

Arquivos deste adendo: internal/matching/{matcher.go,repository.go,spatial.go,direction.go,direction_test.go}, cmd/matchroutes/main.go, teste PostgreSQL Laravel e este relatório. O relatório anterior permanece como histórico da primeira entrega; este adendo atualiza sua descrição do matcher.

## Comandos manuais

No terminal com Go 1.26 e variáveis PostgreSQL já configuradas, sem carregar/imprimir arquivos de credenciais. A conta precisa de SELECT nas tabelas base GTFS, feed_versions, lines e bus_stops. Não reutilizar a conta da API restrita nem ampliar seus grants automaticamente.

```bash
cd mobility_processor
go run ./cmd/matchroutes --feed-version=1 --dry-run --line-id=1
go run ./cmd/matchroutes --feed-version=1 --dry-run --line-id=2
```

Verificar o candidato 1655 no relatório da linha 1 e o 1656 no da linha 2, comparando também os candidatos inversos. Se headsign e referências orientadas estiverem ausentes, esperar insufficient, mesmo com score 1. Para concluir a hipótese, revisar os rótulos operacionais por CL e obter evidência de orientação/terminais; não preencher referências ou confirmar por adivinhação.

Testes unitários Go, que não acessam banco:

```bash
go test ./internal/matching
```

Teste Laravel, exclusivamente no executor já configurado para banco isolado com schema instalado:

```bash
cd ../mobility_backend
POSTGRES_STAGE1_TESTS=1 php vendor/bin/phpunit --configuration phpunit.postgresql.xml --filter JourneyPatternContractTest
```

Nenhum desses comandos foi executado pelo Codex. Foram realizados somente leitura de código/GTFS, edição, gofmt e revisão estática do diff. Não houve execução de testes, migrations, escrita no banco, publicação, commit ou PR. Não há comando de confirmação/publicação nesta entrega: ambos continuam pendentes de validação operacional explícita.
