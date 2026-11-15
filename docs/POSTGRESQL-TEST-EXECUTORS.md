# Executores PostgreSQL isolados — P0.4O.2

Configuração preparada; nenhum Docker, SQL, banco, role ou migration foi executado nesta fase.
Usar somente docker-compose.postgresql-test.yml, projeto open-transit-tests, sem combinar
com os Compose principal/infra. A configuração externa de testes não deve ser um .env real
da aplicação. Logins e senhas precisam ser novos e exclusivos; não reutilizar produção.

## Provisionamento posterior obrigatório

Em autorização futura, criar ambos os bancos no cluster exclusivo de testes e provisionar:

- Logins com prefixo reservado `ot_test_`, com senhas em arquivos próprios legíveis pelo UID
  executor. O provisionador tem credencial distinta dos executores comuns.
- Em **cada banco**, schema `test_control` e tabela `test_control.cluster_identity` com
  `singleton boolean PRIMARY KEY CHECK(singleton)`, `cluster_id text NOT NULL` e
  `purpose text NOT NULL`. Inserir exatamente uma linha, singleton=true, purpose
  `open-transit-tests`, e cluster_id igual a um identificador aleatório exclusivo do
  cluster, fornecido aos executores como `TEST_PG_CLUSTER_ID`.
- Criar o marcador usando o bootstrap isolado **antes** do schema-tools. Não há bypass no
  executor para um marcador ausente. A instalação canônica não cria esse marcador.
- Propriedade do marcador no administrador isolado; revogar CREATE/escrita dos executores
  no schema/tabela e conceder somente USAGE/SELECT necessários. Não conceder memberships
  capazes de assumir o proprietário ou roles administrativas aos executores comuns.
- Instalar explicitamente as migrations nos dois bancos e provisionar os grupos existentes.
  O marcador deve persistir durante testes/rollbacks da trilha canônica.
- Conceder aos logins de testes apenas as operações necessárias às suítes, incluindo as
  trocas de grupo e a instrumentação DDL controlada do Go. Não confundir esses logins
  com a role mínima da aplicação em produção. DDL de fixtures não autoriza alterar o marcador.

O preflight verifica, em transação read-only, database, session_user/current_user, atributos
administrativos e memberships, marcador único e ausência de open_transit no cluster.
Nos executores comuns também exige ausência de escrita no marcador. O provisionador é
exceção explícita à restrição administrativa e só aceita a instalação canônica exata.
Nenhum erro imprime credenciais, nomes dos logins ou mensagens brutas do driver.

## Comandos manuais futuros, não executados

Somente após configuração exclusiva, provisionamento do marcador, logins/grants e bancos:

```sh
# Diagnóstico: não abre conexão, não executa testes.
docker compose --env-file /caminho/config-exclusiva-testes -f docker-compose.postgresql-test.yml run --rm --no-deps laravel-tests php --version
docker compose --env-file /caminho/config-exclusiva-testes -f docker-compose.postgresql-test.yml run --rm --no-deps processor-tests go version

# Instalação explícita, com marcador já existente; repetir para o outro banco permitido.
docker compose --env-file /caminho/config-exclusiva-testes -f docker-compose.postgresql-test.yml run --rm --no-deps -e TEST_PG_EXECUTE=1 -e TEST_PG_PROVISION=1 schema-tools php artisan schema:install-postgresql --force

docker compose --env-file /caminho/config-exclusiva-testes -f docker-compose.postgresql-test.yml run --rm --no-deps -e TEST_PG_EXECUTE=1 laravel-tests php vendor/bin/phpunit --configuration phpunit.postgresql.xml
docker compose --env-file /caminho/config-exclusiva-testes -f docker-compose.postgresql-test.yml run --rm --no-deps -e TEST_PG_EXECUTE=1 processor-tests go test ./...
```

O Go preflight usa `go run ./cmd/checktestcluster` com dependências já presentes no target
processor-tools e GOFLAGS=-mod=readonly. Não altera o módulo nem carrega dotenv.

Os scripts antigos compartilhados agora recusam comandos de banco do Compose legado:
usar o ambiente isolado e seus opt-ins. Scripts de startup de produção não foram alterados.

## Limites

Validação desta entrega é estática e por cenários inválidos sem conexão. A consulta de
identidade, autenticação, grants e suites ainda exigem validação operacional autorizada.
O marcador é um identificador, não um segredo nem prova criptográfica: um administrador
pode copiá-lo. O isolamento de rede/volume e a custódia das credenciais continuam essenciais.
Os comandos executados depois do preflight são código confiável do repositório; isto não é
uma sandbox para código hostil ou overrides arbitrários de Docker. `go test` pode criar
fixtures e instrumentação; não executar suites concorrentes no mesmo banco.
