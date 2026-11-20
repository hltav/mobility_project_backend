# Revisão de sentidos — 1012-10

Inspeção estática e fixtures; nenhum teste, build, dry-run, acesso PostgreSQL,
Valkey ou SPTrans foi executado pelo Codex. Nenhuma confirmação ou escrita em
dados operacionais foi realizada. Não há regra específica da 1012 em produção.

## Resultado com as evidências fornecidas

Linha interna 9 = CL 35254 / SL 2; linha interna 10 = CL 2486 / SL 1.
Feed 1 / rota 1 / pattern 1 tem GTFS direction_id 0, headsign Jd. Monte Belo,
primeira parada Terminal Jardim Britânia, última R. Domênico Aspari, 1 e 41
paradas. Isso descreve a orientação GTFS, mas não prova a orientação operacional
dos CLs. Sem headsign operacional orientado, terminais físicos orientados ou
sequência operacional comprovada, ambos continuam candidatos de identidade
1012-10 e ficam pending / insufficient. A hipótese CL 35254 não foi confirmada.
Não atribuir direction_id 0 a SL 1 nem a SL 2 por aritmética.

## O mecanismo existente

- Seleção exata pelo código completo, feed explícito e provider. Sem exato, há
  fallback textual identificado; código não determina orientação nem variante.
- Identidade inclui CL e SL cadastrados, mas seus números não são score de direção.
- HeadSign é public.lines.headsign explícito. TP/TS e aliases origin/destination
  não são tratados como origem/destino orientados, nem invertidos pelo número SL.
- Terminais físicos usam origin_stop_id/destination_stop_id e coordenadas das
  respectivas paradas canônicas. Nomes são relatados; sem coordenadas, não formam
  automaticamente um par geográfico orientado. origin_lat/lng legados não
  substituem esses terminais.
- Shape válido deve alinhar início/fim às primeiras/últimas paradas GTFS. O
  matcher mantém sua tolerância existente de 500 m para terminais; isso é evidência
  de revisão, não prova de identidade de uma plataforma.
- Paradas operacionais vêm de associações SPTrans/CL/CP confirmadas. Como
  line_stop_sources não fornece sequência oficial, o loader as marca sequence=null.
  ORDER BY de IDs não cria ordem operacional. São cobertura, não prova de sentido.
- Quando uma sequência operacional explicitamente comprovada estiver disponível,
  compareStops já compara progressão: pelo menos três posições, cobertura 80%,
  tolerância 100 m e ambiguidade quando posições concorrentes diferem menos de
  10 m. Circulares exigem progressão, permitindo uma volta. Não foram alteradas
  essas tolerâncias nem inventada sequência operacional nesta entrega.
- Orientação pode ser supported por headsign explícito, par de terminais físicos
  ou progressão. Evidência contrária prevalece. Tudo permanece status pending;
  o código nunca promove uma sugestão a confirmed.

## Lacuna corrigida pontualmente

Assess avaliava uma linha por vez e só reconhecia múltiplos patterns concorrentes
para essa linha. O mesmo pattern podia aparecer supported para SL 1 e SL 2 sem
alerta entre linhas, ainda que ambos permanecessem pending.

AssessBatch reutiliza Assess e marca ambiguous quando o mesmo feed/rota/pattern,
provider e código completo recebem suporte para CLs diferentes de sentidos
opostos. Não escolhe vencedor por maior score, menor ID ou número do sentido.
Mantém os scores individuais como evidência, zera confidence_score final do
conflito e inclui conflicting_operational_line_ids e motivo no relatório.

RunAll e o CLI usam a mesma avaliação em lote. O dry-run com --line-id carrega
também os pares do mesmo código para detectar esse conflito, mas só apresenta
assessments da linha solicitada. comparison_line_ids identifica quem foi
comparado. Snapshot continua em transação READ ONLY / REPEATABLE READ.
Sugestões pending para ambos os CLs podem existir quando falta evidência; isso
não constitui atribuição operacional confirmada. SaveMatches continua preservando
registros confirmed/rejected existentes, sem rebaixá-los ou sobrescrevê-los.

## Validação manual no ambiente do Processor principal

A imagem atual contém o servidor, mas não o binário matchroutes nem a toolchain
Go. Usar o Go já instalado no Ubuntu para compilar estaticamente o CLI existente
e copiá-lo para /tmp do container principal, sem criar containers auxiliares.
Estes comandos não foram executados pelo Codex:

```bash
cd ~/Projects/OPEN_TRANSIT/mobility_project/mobility_processor

# Testes locais com fixtures sintéticas, sem serviços externos:
go test ./internal/matching -count=1 -v

# Preparar somente o CLI existente para a arquitetura nativa do host/container:
CGO_ENABLED=0 go build -trimpath -o /tmp/ope38-matchroutes ./cmd/matchroutes
docker cp /tmp/ope38-matchroutes mobility_processor:/tmp/ope38-matchroutes

# Mesmas configurações PostgreSQL já presentes no Processor principal:
docker exec mobility_processor /tmp/ope38-matchroutes \
  --feed-version 1 --dry-run --line-id 9 > /tmp/ope38-line-9.json
docker exec mobility_processor /tmp/ope38-matchroutes \
  --feed-version 1 --dry-run --line-id 10 > /tmp/ope38-line-10.json

python3 -m json.tool /tmp/ope38-line-9.json
python3 -m json.tool /tmp/ope38-line-10.json
```

Requer permissões SELECT nas tabelas operacionais/GTFS pelo papel configurado no
Processor. Não consultar a API SPTrans para esse diagnóstico. Não usar
--confirm-write nesta validação. A referência feed=1 é parâmetro manual, não
default de produção. O timeout do CLI já existente é de cinco minutos.

Examinar feed/rota/pattern, CL/SL, operational_headsign, terminais físicos,
stop_evidence, missing_evidence, orientation_score, evidence_status e conflitos.
Se somente GTFS tiver nome/terminais orientados, insufficient é o resultado
seguro. Se ambos os CLs tiverem o mesmo headsign explícito, o conflito deve ser
revisado, não resolvido numericamente. Os fixtures com coordenadas/ordem sintéticas
demonstram possibilidades do algoritmo; não validam a hipótese no banco real.

## Lote posterior

```bash
docker exec mobility_processor /tmp/ope38-matchroutes \
  --feed-version 1 --dry-run > /tmp/ope38-matching-feed-1.json
```

Sem --line-id, o mesmo matcher compara todas as linhas e assinala conflitos entre
sentidos. Cada variante e feed permanece separado. Revisar os relatórios antes
de qualquer etapa de escrita. O modo de escrita existente só salva pending;
publicação e confirmação explícita de associações são processos separados.

## Testes preparados

reference1012_test.go cobre os dois CLs e um único pattern de 41 paradas: metadados
sem evidência operacional; sequência sintética que suporta SL 2 / GTFS 0 e
contradiz SL 1; suporte simultâneo aos dois sentidos marcado ambiguous;
independência da ordem das linhas; peer insuficiente; isolamento entre feeds;
preservação de pending e dos IDs. Os testes anteriores continuam cobrindo variantes,
circulares, tolerâncias e ausência de conversão numérica dos sentidos.
Validação real dos dados e execução da suíte permanecem pendentes.
