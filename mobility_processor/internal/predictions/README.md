# Coleta de previsões sob demanda

O runtime em `cmd/server` habilita o worker somente quando
`SPTRANS_PREDICTIONS_ENABLED=true` (valor literal). Ausência, `false` e valores
desconhecidos mantêm a funcionalidade desligada, sem worker nem registro de demanda.

`GET /buses/{CL}` registra somente CL e horário em memória, sem I/O adicional.
Um worker resolve o sentido em `public.lines`, exigindo exatamente um registro
SPTrans com direção 1 ou 2. Ausência, duplicidade, direção inválida ou erro de
consulta impedem a coleta. Não há associação GTFS nem inferência por código.

Política inicial fixa: demanda expira após 2 minutos; até 256 CLs por processo;
um despacho por tick de 1 segundo; intervalo mínimo de despacho por CL de
5 segundos; consulta de identidade com timeout de 2 segundos; uma coleta por vez.
Demandas repetidas atualizam o mesmo registro. Ao atingir a capacidade, novas
linhas são ignoradas até liberar espaço; isso não altera a resposta GPS.

O Collector reutiliza o cliente SPTrans e o cliente Valkey existentes. Continuam
valendo frescor de 20 segundos, retenção de 2 minutos, lease com token, cooldown,
backoff, timeout de 10 segundos e CAS dos snapshots. Falhas do Valkey impedem
chamadas não coordenadas. Erros da coleta não retornam ao handler GPS.
SIGINT/SIGTERM cancelam o worker; o servidor aguarda sua conclusão antes de
fechar PostgreSQL e Valkey. Nenhuma goroutine é criada por linha/requisição.

## Validação manual (não executada pelo Codex)

Na pasta `mobility_processor`, com as dependências locais disponíveis:

```bash
go test ./internal/predictions ./internal/api -count=1 -v
go test -race ./internal/predictions ./internal/api -count=1
go build -o /tmp/ope38-processor ./cmd/server
```

Com as configurações habituais do Processor já fornecidas pelo ambiente:

```bash
# Primeiro validar desligado; encerrar antes de iniciar outra instância.
SPTRANS_PREDICTIONS_ENABLED=false /tmp/ope38-processor
# Ativação explícita, em execução separada após validação:
SPTRANS_PREDICTIONS_ENABLED=true /tmp/ope38-processor
```

Em container, a variável deve ser injetada no ambiente do serviço Processor;
exportá-la somente no host não garante sua transmissão ao container.
Uma consulta ao endpoint existente `/buses/2506` gera demanda para esse CL;
verificar depois o snapshot `sptrans:prediction:v1:cl:2506:sl:1` usando a identidade
operacional efetivamente cadastrada, sem assumir sentido por regra numérica.
Sem demanda por 2 minutos não devem começar novas coletas desse CL. Encerrar com
SIGTERM e verificar a saída do processo. Esses passos habilitados consultam a
SPTrans real e devem ser executados deliberadamente pelo operador.

## Limitações

Demanda é local ao processo e se perde em reinícios. Lease/cache são compartilhados
entre réplicas; a cadência global entre CLs diferentes é por processo. Sob carga,
um único worker pode demorar a atender todas as linhas; este primeiro incremento
prioriza limitar chamadas. Polling exclusivo do Stop Progress ou respostas servidas
inteiramente pelo cache Laravel não renovam a demanda: é necessário que o fluxo
GPS consulte o endpoint Processor existente. O worker não calcula minutos, não
homologa horários brutos e não integra o snapshot ao Laravel.
