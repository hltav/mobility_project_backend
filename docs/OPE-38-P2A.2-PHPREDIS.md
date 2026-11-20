# OPE-38 P2A.2 — PhpRedis no runtime Laravel

## Causa e alteração

Diagnóstico de runtime fornecido pelo operador: client=phpredis, prefix vazio, DB 0, PhpRedis e Predis ausentes. A chave buses:line:2506 existe no Valkey. O erro Class "Redis" not found ocorre porque o cliente selecionado requer uma extensão não instalada; o prefixo não é a causa comprovada neste ambiente.

O Dockerfile do backend instalava as extensões existentes, mas não redis. A alteração acrescenta `pecl install redis-6.1.0` e `docker-php-ext-enable redis` ao estágio final `php:8.3-fpm`, antes da instalação Composer. A versão 6.1.0 foi fixada para evitar depender de uma versão PECL futura durante o build; pertence à série 6 aceita pela dependência Laravel registrada no composer.lock. Não altera a extensão CLI isolada postgres-tools, a lista de extensões já instalada, o cliente phpredis, o prefixo, o DB, os contratos ou o Processor. Não adiciona Predis, nova conexão ou dependência Composer.

Arquivos desta entrega: mobility_backend/Dockerfile e este documento. Alterações locais anteriores preservadas. Somente revisão estática: nenhum build, teste, serviço, chamada externa, escrita no banco/Valkey, migration, commit ou PR foi executado. A resolução do erro depende da validação real da imagem reconstruída.

## Validação manual

Executar na raiz mobility_project, com as mesmas variáveis, arquivos Compose e nome de projeto já usados pelo ambiente. Os arquivos abaixo correspondem aos serviços locais encontrados: backend, processor, postgres, valkey e o override local da prévia. Não usar merged.yml para inspecionar ou imprimir configurações/credenciais. Se o ambiente usa outros overrides, preservar os mesmos no comando.

Definir um atalho apenas para evitar repetir os arquivos:

```bash
ope38_compose() {
  docker compose -f docker-compose.infra.yml -f docker-compose.yml -f docker-compose.gtfs-local.yml "$@"
}
```

1. Reconstruir somente a imagem backend:

```bash
ope38_compose build backend
```

Build da imagem não atualiza o container existente. Para aplicar manualmente a imagem, recriar apenas o backend, mantendo as dependências já em execução:

```bash
ope38_compose up -d --no-deps --no-build --force-recreate backend
```

Esse segundo comando é uma ação operacional a ser executada pelo usuário, não foi executado pelo Codex. Não inicia/recria Processor, PostgreSQL ou Valkey. O entrypoint atual não executa migrations.

2. Confirmar PHP 8.3 e a extensão no container final:

```bash
ope38_compose exec -T backend php -r 'var_export(["php"=>PHP_VERSION,"redis_loaded"=>extension_loaded("redis"),"redis_version"=>phpversion("redis"),"redis_class"=>class_exists("Redis")]); echo PHP_EOL;'
```

Esperado após build/aplicação bem-sucedidos: PHP 8.3, redis_loaded=true, redis_version=6.1.0, redis_class=true.

3. Confirmar a leitura Laravel pelo cliente padrão, sem imprimir conteúdo GPS ou credenciais:

```bash
ope38_compose exec -T backend php artisan tinker --execute='
$r = \Illuminate\Support\Facades\Redis::connection();
$raw = $r->get("buses:line:2506");
$positions = is_string($raw) ? json_decode($raw, true) : null;
dump([
    "client" => config("database.redis.client"),
    "prefix" => config("database.redis.options.prefix"),
    "db" => config("database.redis.default.database"),
    "exists" => (int) $r->exists("buses:line:2506"),
    "ttl" => $r->ttl("buses:line:2506"),
    "json_list" => is_array($positions) && array_is_list($positions),
    "vehicle_count" => is_array($positions) ? count($positions) : null,
]);'
```

Esperado com snapshot vigente: client=phpredis, prefix vazio, db=0, exists=1, TTL positivo e json_list=true. A chave dura 60 s e precisa estar sendo atualizada pelo fluxo normal de ônibus. exists=0/TTL=-2 após expiração não implica falha da extensão. Um resultado de leitura válido sem Class "Redis" not found é uma verificação distinta da disponibilidade do snapshot naquele instante.

4. Com API_BASE e TOKEN definidos pelo operador, consultar a prévia autenticada:

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$API_BASE/api/v1/gtfs/preview/feeds/1/patterns/1655/lines/1/vehicles/stop-progress"
```

A autenticação, a prévia local e o snapshot precisam continuar válidos. Com snapshot legível, vehicle_snapshot_unavailable deve deixar de ocorrer; o resultado por veículo ainda pode ser stale, ambiguous ou unavailable por outras regras já existentes. Um cálculo utilizável nessa prévia permanece provisional. Não esperar matched operacional sem confirmação e publicação; esta alteração não modifica essas regras.

Se a etapa 2 continuar indicando extensão ausente, conferir se o container foi realmente recriado a partir da nova imagem e se não há um target/imagem diferente selecionado no deploy. Se a etapa 3 falhar por autenticação ou conexão, investigar isso separadamente sem alterar prefixo/DB por tentativa nem imprimir senhas/URLs completas.
