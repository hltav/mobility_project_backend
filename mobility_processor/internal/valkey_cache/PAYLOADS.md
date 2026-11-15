════════════════════════════════════════════════════════════════════════════════
 EXEMPLOS REAIS DE PAYLOADS JSON NO CACHE
════════════════════════════════════════════════════════════════════════════════

Este arquivo documenta os formatos JSON exatos que são armazenados em cache.
Cada exemplo é precedido pela chave de cache (KeyXxx) que o armazena.

════════════════════════════════════════════════════════════════════════════════
LINHAS (Lines) — TTL: 60 dias
════════════════════════════════════════════════════════════════════════════════

KEY: lines:id:33624
TIPO: CachedLine
ARMAZENADO POR: POST /sync (background)
RECUPERADO POR: GET /lines/resolve, POST /nearby/spatial

JSON:
{
  \"code\": \"8000-10\",
  \"name\": \"Av. Paulista - Terminal Parque Dom Pedro\",
  \"origin\": \"Av. Paulista\",
  \"destination\": \"Terminal Parque Dom Pedro\",
  \"circular\": false,
  \"sptrans_id\": 33624,
  \"updated_at\": \"2026-06-03T14:30:00Z\"
}

───

KEY: lines:code:8000-10:dir:0
TIPO: int (sptrans_id)
ARMAZENADO POR: POST /sync (background)
RECUPERADO POR: GET /lines/resolve

JSON:
33624

Explicação: Chave simples que resolve code+direction para sptrans_id.
Permite rápida busca de ID sem desserializar toda a linha.

════════════════════════════════════════════════════════════════════════════════
PARADAS (Stops) — TTL: 30 dias
════════════════════════════════════════════════════════════════════════════════

KEY: stops:cp:1023
TIPO: CachedStop
ARMAZENADO POR: POST /sync/stops (background)
RECUPERADO POR: POST /stops/nearby

JSON:
{
  \"sptrans_code\": 1023,
  \"name\": \"Avenida Paulista\",
  \"address\": \"Avenida Paulista, 1000, São Paulo\",
  \"lat\": -23.5614,
  \"lng\": -46.6558,
  \"updated_at\": \"2026-06-03T14:35:00Z\"
}

───

KEY: stops:line:1
TIPO: []CachedStop (array)
ARMAZENADO POR: POST /sync/stops (background)
RECUPERADO POR: Futuro, para filtros por linha

JSON:
[
  {
    \"sptrans_code\": 1023,
    \"name\": \"Avenida Paulista\",
    \"address\": \"Avenida Paulista, 1000\",
    \"lat\": -23.5614,
    \"lng\": -46.6558,
    \"updated_at\": \"2026-06-03T14:35:00Z\"
  },
  {
    \"sptrans_code\": 1024,
    \"name\": \"Rua Augusta\",
    \"address\": \"Rua Augusta, 2000\",
    \"lat\": -23.5620,
    \"lng\": -46.6560,
    \"updated_at\": \"2026-06-03T14:35:00Z\"
  }
]

───

KEY: stops:nearby:-2356:-4665:100
TIPO: []CachedStop (array, truncado a 2 casas decimais + raio em km*100)
ARMAZENADO POR: POST /stops/nearby (background, TTL=6h)
RECUPERADO POR: POST /stops/nearby

JSON:
[
  {
    \"sptrans_code\": 1023,
    \"name\": \"Avenida Paulista\",
    \"address\": \"Avenida Paulista, 1000\",
    \"lat\": -23.5614,
    \"lng\": -46.6558,
    \"updated_at\": \"2026-06-03T14:35:00Z\"
  },
  {
    \"sptrans_code\": 1025,
    \"name\": \"Consolação\",
    \"address\": \"Rua Consolação, 500\",
    \"lat\": -23.5632,
    \"lng\": -46.6520,
    \"updated_at\": \"2026-06-03T14:35:00Z\"
  }
]

════════════════════════════════════════════════════════════════════════════════
GEOCODING — TTL: 30 dias
════════════════════════════════════════════════════════════════════════════════

KEY: geocode:addr:7f1a5bc3d89e2f4a
      (hash SHA1 do endereço normalizado \"avenida paulista, 1000, são paulo\")
TIPO: CachedGeocodeResult
ARMAZENADO POR: POST /geocode (background)
RECUPERADO POR: POST /geocode

JSON:
{
  \"lat\": -23.5614,
  \"lng\": -46.6558,
  \"address\": \"Avenida Paulista, 1000, São Paulo\",
  \"updated_at\": \"2026-06-03T14:35:00Z\"
}

Explicação: A chave é hash SHA1 do endereço NORMALIZADO (lowercase, trimmed).
Garante que \"Avenida Paulista\" e \"avenida paulista\" resolvam para a mesma chave.

════════════════════════════════════════════════════════════════════════════════
LINHAS PRÓXIMAS — TTL: 6 horas
════════════════════════════════════════════════════════════════════════════════

KEY: nearby:lines:-23.5614:-46.6558:1.00
      (lat=-23.5614, lng=-46.6558, raio=1.00km)
TIPO: []CachedNearbyLine (array com distâncias)
ARMAZENADO POR: POST /nearby/spatial (background, TTL=6h)
RECUPERADO POR: POST /nearby/spatial

JSON:
[
  {
    \"id\": 1,
    \"code\": \"8000-10\",
    \"name\": \"Av. Paulista - Terminal\",
    \"origin\": \"Av. Paulista\",
    \"destination\": \"Terminal Parque Dom Pedro\",
    \"circular\": false,
    \"sptrans_id\": 33624,
    \"origin_lat\": -23.5610,
    \"origin_lng\": -46.6550,
    \"distance_km\": 0.12
  },
  {
    \"id\": 2,
    \"code\": \"702U-10\",
    \"name\": \"Consolação - Terminal São Mateus\",
    \"origin\": \"Consolação\",
    \"destination\": \"Terminal São Mateus\",
    \"circular\": false,
    \"sptrans_id\": 33625,
    \"origin_lat\": -23.5630,
    \"origin_lng\": -46.6520,
    \"distance_km\": 0.28
  }
]

Explicação: Distance_km é pré-calculada no DB, não usa Haversine.
Resultado é ordenado por distância (mais próximo primeiro).

════════════════════════════════════════════════════════════════════════════════
ÔNIBUS (BUS POSITIONS) — TTL: 60 segundos
════════════════════════════════════════════════════════════════════════════════

KEY: buses:line:33624
TIPO: []CachedBusPosition (array)
ARMAZENADO POR: Não implementado ainda (volátil demais)
RECUPERADO POR: GET /buses/{sptrans_id}

JSON:
[
  {
    \"prefix\": \"5037\",
    \"accessible\": false,
    \"captured_at\": \"2026-06-03T14:35:12Z\",
    \"lat\": -23.5620,
    \"lng\": -46.6550
  },
  {
    \"prefix\": \"5038\",
    \"accessible\": true,
    \"captured_at\": \"2026-06-03T14:35:10Z\",
    \"lat\": -23.5640,
    \"lng\": -46.6530
  }
]

Explicação: Dados MUITO voláteis (60s TTL), não implementado em cache por enquanto.
Se necessário, adicionar cache no GET /buses/{id}, mas com TTL muito curto.

════════════════════════════════════════════════════════════════════════════════
METADATA — TTL: Sem TTL (manual)
════════════════════════════════════════════════════════════════════════════════

KEY: meta:cache:version
TIPO: CacheMetadata
ARMAZENADO POR: Futuro (admin/healthcheck)
RECUPERADO POR: Healthcheck, admin panels

JSON:
{
  \"version\": \"1.0.0\",
  \"lines_synced_at\": \"2026-06-03T14:30:00Z\",
  \"stops_synced_at\": \"2026-06-03T14:35:00Z\",
  \"total_lines\": 12500,
  \"total_stops\": 18000
}

Explicação: Metadata é atualizada por POST /sync e POST /sync/stops.
Permite rastrear quando dados foram sincronizados pela última vez.

════════════════════════════════════════════════════════════════════════════════
ESTRUTURA DE ARMAZENAMENTO NO DISCO
════════════════════════════════════════════════════════════════════════════════

Cada chave é armazenada em um arquivo SHA1-hashed em /tmp/valkey/
(ou VALKEY_STORAGE_PATH se definido)

Arquivo em disco:
  Filename: <sha1_hash_of_key>.json
  Content: {
    \"expires_at\": <unix timestamp>,
    \"data\": <RawJSON string>
  }

Exemplo para KEY=lines:id:33624:
  Filename: b8d4c5a9f3e21b75c6a8d9e2f1a4b5c6d7e8f9a0.json
  Content:
  {
    \"expires_at\": 1718620200,
    \"data\": \"{\\\"code\\\":\\\"8000-10\\\", ... }\"
  }

Quando cache.GetJSON() lê:

  1. Calcula SHA1(key) → filename
  2. Lê arquivo
  3. Desserializa JSON
  4. Verifica expires_at > now()
  5. Se OK, deserializa data em tipo Go
  6. Se expirado, deleta arquivo + retorna miss

════════════════════════════════════════════════════════════════════════════════
TAMANHO ESTIMADO DOS PAYLOADS
════════════════════════════════════════════════════════════════════════════════

Tipo                    Exemplo Size    Notas
──────────────────────────────────────────────────────────────────────────────
CachedLine              ~200 bytes      Linha única com nome/origem/destino
CachedStop              ~250 bytes      Parada com nome/endereço/coords
CachedGeocodeResult     ~150 bytes      Apenas coords + endereço
CachedNearbyLine        ~280 bytes      Com distance_km calculado
CachedBusPosition       ~180 bytes      Prefix + coords + accessible flag

Array de 100 stops      ~25 KB          Razoável por 30 dias TTL
Array de 50 linhas      ~10 KB          Razoável por 6 horas TTL

Total estimado:

- All lines (12k × 200B) ≈ 2.4 MB
- All stops (18k × 250B) ≈ 4.5 MB
- Geocode cache (3k × 150B) ≈ 450 KB
- Nearby queries (100s × 25KB) ≈ 2.5 MB
  ─────────────────────────────────────
  Total ~9.9 MB para disco (tolerável, rápido)

════════════════════════════════════════════════════════════════════════════════
