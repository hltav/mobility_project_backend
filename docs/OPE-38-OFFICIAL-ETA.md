# ETA oficial no Stop Progress

Alteração aditiva, sem endpoint novo e sem chamadas SPTrans no Laravel. Nenhum
teste, build, acesso Valkey/PostgreSQL ou serviço foi executado pelo Codex.

## Componentes reutilizados e identidade

`VehicleProgressService` mantém projeção, GPS, próxima parada e distâncias de
`VehicleProgressRepository`. O enriquecimento exige match_status matched,
publication_status active e matching_status confirmed do Journey Pattern existente.
`OperationalStopRepository::confirmedPatternCodes` apenas resolve referências
existentes: feed/pattern/linha operacional → stop GTFS → referência GTFS confirmed
→ mesma parada canônica → referência CP SPTrans confirmed → associação CL/CP
confirmed em line_stop_sources. Revalida publicação, mapping vigente, provider,
direções cadastradas, feed e ID externo. Não gera candidatos nem faz matching.

Múltiplos CPs na mesma ocorrência ou CP repetido no pattern ficam indisponíveis:
a previsão oficial não traz identificador de ocorrência. Prefixos são textuais,
comparados exatamente, sem conversão numérica. Previsões duplicadas para CP/prefixo
também são recusadas. Estado ambíguo/stale/unavailable de GPS não recebe minutos.

`ValkeyService::getPredictionSnapshot` usa a conexão padrão existente e lê
`sptrans:prediction:v1:cl:<CL>:sl:<SL>` no máximo uma vez por requisição elegível,
depois de resolver os vínculos necessários. Não há leitura extra por veículo ou
parada, alteração de TTL, gravação nem fallback à API SPTrans.

## Prévia e bloqueios concretos

A prévia existente declara matching_status unverified e associação espacial
provisional. Essa seleção não prova vínculo operacional CL/sentido ↔ pattern.
Logo ela preserva GPS/progresso/distâncias e retorna por veículo:

```json
{
  "eta_minutes": null,
  "eta": {
    "status": "unavailable",
    "source": "sptrans",
    "reason": "preview_pattern_operational_link_unconfirmed",
    "reference_stop_code": null,
    "predicted_arrival_at": null,
    "eta_seconds": null,
    "eta_minutes": null,
    "observed_at": null
  }
}
```

Conforme as evidências fornecidas, falta mapping feed 1/pattern 1655/linha 1 e
publicação do feed 1. O endpoint operacional continua dependente dessas condições;
também é necessária referência GTFS confirmed para a parada canônica do CP.
Um snapshot real CP/CL/prefixo não fornece sozinho esse vínculo. O componente de
candidatas existente é diagnóstico, não confirmação, e não é usado como fallback.

## Contrato para os casos comprovados

O ETA do veículo é para seu next_stop, igual ao primeiro upcoming_stop. Não é ETA
para uma parada escolhida pelo passageiro. Exemplo sintético com todas as
associações comprovadas e relógio de requisição em 2026-11-20T03:00:20Z:

```json
{
  "vehicle_prefix": "12601",
  "eta_minutes": 2,
  "eta": {
    "status": "available",
    "source": "sptrans",
    "reason": null,
    "reference_stop_code": 700016623,
    "predicted_arrival_at": "2026-11-20T00:02:00-03:00",
    "eta_seconds": 100,
    "eta_minutes": 2,
    "observed_at": "2026-11-20T03:00:15+00:00"
  }
}
```

next_stop e cada upcoming_stop elegível recebem os mesmos campos aditivos eta e
eta_minutes para sua própria parada. Sem vínculo, o ETA do veículo é unavailable;
as coleções existentes continuam presentes sem mudança de significado. Versão 1
mantida por se tratar de extensão aditiva. Os demais campos não foram removidos.

## Política temporal conservadora

`OfficialPredictionEta` só interpreta tempo; não associa fontes. Valida schema 1,
source sptrans, CL/SL e status available. Exige now < valid_until e idade do
fetched_at inferior a 20 segundos, independentemente da retenção física de 2 min.
Snapshot ausente, vazio, provider_error, inválido ou futuro não produz minutos.

Requer ta datado com offset/Z; usa America/Sao_Paulo para hr/t. hr deve resolver
uma única data entre o dia de fetched_at e seus vizinhos, a até 90 s da coleta e
120 s da captura. Captura até 120 s anterior ou 1 s posterior à coleta é aceita.
Esses limites são conservadores iniciais, não homologação do fornecedor.
Aceita relógios HH:mm ou HH:mm:ss. Sem contexto datado suficiente, não infere data.
Virada de dia é aceita somente com horizonte de até 2 h a partir de hr. Previsão
já passada é unavailable, sem transformá-la em 24 h ou inventar zero minuto.

eta_seconds é chegada prevista menos horário da requisição; eta_minutes usa ceil
dos segundos/60. Os minutos são derivados do horário oficial, sem modelo de
velocidade ou distância. O formato temporal real ainda exige homologação manual.

## Testes preparados, não executados

```bash
cd mobility_backend
php artisan test --filter='OfficialPredictionEtaTest|ValkeyPredictionSnapshotTest|VehicleProgressServiceTest'
```

Fixtures sintéticas e mocks cobrem frescor/expiração, CL/SL/CP/prefixo divergentes,
duplicação, cache ausente/inválido/indisponível, meia-noite, captura sem data,
leitura única para múltiplos veículos e preservação do progresso. O SQL de
referências e os minutos reais dependem de validação manual em ambiente controlado.
Não executar alterações de publicação/mapping/referências como parte deste teste.
