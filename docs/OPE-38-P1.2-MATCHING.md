# OPE-38 P1.2 — seleção e evidências de matching

## Diagnóstico

O conjunto de 26 candidatos resultava da avaliação de cada pattern isoladamente: o código exato de uma rota não impedia o fallback textual das outras. O antigo confidence_score também representava identidade por código, mesmo sem orientação. O matcher não lia paradas operacionais confirmadas para avaliar cobertura.

Rastreabilidade dos campos: RawLine recebe CL, SL, TP e TS da resposta SPTrans; UpsertFromSPTrans grava CL em sptrans_line_id, SL em sptrans_direction, TP em headsign_primary_to_secondary e TS em headsign_secondary_to_primary. O schema gera origin/destination a partir dessas duas últimas colunas. Não são evidências adicionais nem campos orientados independentes. O importador não preenche headsign, origin_stop_id ou destination_stop_id. origin_lat/origin_lng podem vir de geocodificação do rótulo TP, sem orientação comprovada.

O contrato local descreve TP/TS como letreiros principal/secundário e SL como 1/2. Não há referência primária local comprovando a orientação dos dois rótulos por SL. Não foi consultado serviço remoto nem lido arquivo de credenciais para suprir essa lacuna. Portanto não se criou conversão TP/TS→origem/destino por SL, nem SL−1→GTFS. A nomenclatura das colunas direcionais é enganosa frente ao escritor efetivo e não foi tomada como prova.

## Correções no matcher existente

1. Seleção antes da avaliação: filtra feed explicitamente selecionado e provider compatível, então retém todos os patterns de código completo exato. Prefixos e sufixos diferentes, inclusive zeros iniciais, não são equivalentes. Se houver código exato, nenhum candidato por similaridade entra. Sem código exato, fallback textual explícito, limiar 0,6; não usa coordenadas geocodificadas de TP como evidência de identidade/orientação.
2. Provider representa a fonte, não necessariamente a empresa operadora. A consulta preserva r.agency_id e apresenta gtfs_agency_id. public.lines não possui identificador de agência operacional; operational_agency_identity permanece ausente no diagnóstico. Agências/variantes de mesmo código continuam separadas, sem confirmação implícita por provider.
3. identity_score mede a descoberta da linha (código exato = 1). orientation_score mede evidência orientada: 0 sem comprovação, 0,6 por headsign explícito coincidente, 0,8 por par de terminais orientados, 0,9 por progressão de paradas oficialmente ordenadas. São pesos conservadores de diagnóstico, não probabilidades calibradas. confidence_score é o mínimo dos dois scores; código exato sozinho resulta em 0 de confiança conjunta.
4. Carrega paradas operacionais somente com line_stop_sources e stop_source_references SPTrans confirmados, do CL correspondente. Não usa associações GTFS como prova independente de si mesmas. Paradas GTFS permanecem ordenadas por pattern_sequence e escopadas ao feed/pattern.
5. Compara posições operacionais com as paradas oficiais do pattern para cobertura (até 100 m). Não afirma distância direta de cada parada operacional à polilinha inteira: a evidência geométrica continua sendo a consistência entre extremos do shape e extremos das paradas GTFS (até 500 m). Cobertura de paradas e orientação são critérios separados.
6. O banco atual contém associações operacionais sem sequência; elas são carregadas com sequence=null e jamais ordenadas por ID/proximidade como se fosse ordem oficial. Nesse caso, a cobertura é informada, mas progression=unordered não aumenta o score de orientação. A comparação de progressão aceita uma ordem explicitamente fornecida ao modelo, com ao menos três correspondências, cobertura mínima de 80% e sem ambiguidades de ocorrência; não há nova fonte persistida de sequência nesta entrega.
7. Ocorrências GTFS repetidas ou plataformas indistinguíveis (diferença inferior a 10 m entre duas distâncias candidatas) não estabelecem progressão única. Linhas circulares exigem evidência ordenada; admitem uma passagem pelo início do pattern, sem deduzir orientação de terminais coincidentes. Múltiplos patterns sustentados continuam ambiguous, sem seleção pelo menor ID ou pelo número de direção.
8. Mantém supported/insufficient/contradicted/ambiguous como avaliações para revisão, sempre status=pending. Não promove mappings. O modo escritor já existente reaproveita SaveMatches, continua escrevendo somente pending e preserva decisões confirmed/rejected; não foi executado. Não limpa sugestões antigas.

## Diagnóstico somente leitura

O --dry-run mantém READ ONLY / REPEATABLE READ e não chama SaveMatches. O relatório acrescenta operational_lines, feed_scope=explicit_ready_feed, operational_code, gtfs_code, provider, gtfs_agency_id, candidate_selection, identity_score, orientation_score, stop_evidence, available_evidence, missing_evidence, reasons e variant_status. Conserva CL, SL, feed, rota, pattern, direção GTFS, headsigns, rótulos brutos e terminais.

O feed permanece selecionado explicitamente e precisa estar ready. Esse modo não significa publicação ativa. Nenhum endpoint Laravel foi alterado: consulta operacional continua exigindo mapping confirmed válido e publicação ativa.

Exemplo parcial esperado diante do estado operacional descrito, não coletado após esta alteração:

```json
{
  "operational_code": "8000-1",
  "gtfs_code": "8000-1",
  "feed_version_id": 1,
  "route_id": 971,
  "pattern_id": 1655,
  "candidate_selection": "exact_code",
  "identity_score": 1,
  "orientation_score": 0,
  "confidence_score": 0,
  "status": "pending",
  "evidence_status": "insufficient"
}
```

Para cada ID operacional, se feed/provider/código permanecerem como informados, espera-se apenas os patterns 1655 e 1656 na lista, sem as outras rotas. Não se afirma que ambos os sentidos já foram decididos: os dados orientados continuam faltando.

## Arquivos desta etapa

- mobility_processor/internal/matching/matcher.go — seleção global por identidade e fallback explícito.
- mobility_processor/internal/matching/repository.go — provider/agência e paradas com procedência.
- mobility_processor/internal/matching/direction.go — scores separados e explicações de evidência.
- mobility_processor/internal/matching/stops.go — cobertura e progressão conservadoras no mesmo pacote.
- mobility_processor/internal/matching/selection_test.go — testes unitários novos.
- mobility_processor/cmd/matchroutes/main.go — contexto operacional no relatório dry-run.
- mobility_processor/internal/integration/postgresql_test.go — fixture de matching ajustado para provider sptrans, acompanhando a nova exigência de compatibilidade; sem alteração do schema ou execução do teste.
- Este relatório. Relatórios anteriores descrevem etapas anteriores e não substituem estas regras atualizadas.

## Validação manual, sem execução pelo Codex

No terminal com Go 1.26 e conexão PostgreSQL já configurada pelas variáveis existentes, com permissões SELECT para as tabelas consultadas:

```bash
cd mobility_processor
go run ./cmd/matchroutes --feed-version=1 --dry-run --line-id=1
go run ./cmd/matchroutes --feed-version=1 --dry-run --line-id=2
go test ./internal/matching
```

Não utilizar --confirm-write para validar o diagnóstico. Não são necessários migrations, importações ou publicação. Os comandos não carregam nem imprimem arquivos de credenciais.

Os testes unitários preparados cobrem supressão de rotas irrelevantes diante de código exato, preservação de variantes/sufixos/zeros iniciais, provider/feed, fallback explícito, TP/TS sem orientação, cobertura sem ordem, progressão direta/inversa, repetições ambíguas, tolerâncias e circular com uma passagem pelo início. Os testes de sentidos da referência 8000-1 já existentes usam evidências operacionais sintéticas, não dados confirmados do banco real.

## Critérios para confirmar os sentidos da 8000-1

Para CL 2506, comprovar destino operacional compatível com Pça. Ramos De Azevedo e orientação Lapa→Centro; para CL 35274, comprovar destino Term. Lapa e orientação Centro→Lapa. Vincular essa prova aos CLs, não apenas ao nome geral da linha. Confrontar com terminais orientados ou sequência operacional documentada, progressão das paradas e geometrias dos patterns 1655/1656 no feed 1. Rever variantes/agência e conflitos antes de qualquer decisão humana.

Pendências reais: semântica operacional documentada de TP/TS por SL; headsign específico por CL ou referências de terminais comprovadas; fonte de sequência oficial operacional, caso necessária; identificador da operadora/agência no lado operacional. A confirmação e a publicação continuam ações separadas e não foram realizadas.

Revisão executada: leitura de código/schema, gofmt e git diff --check. Nenhum teste, benchmark, build, serviço, migration, escrita PostgreSQL/Valkey, commit, push ou PR foi executado. Nenhum resultado de teste é afirmado.
