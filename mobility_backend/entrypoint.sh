#!/bin/sh
set -e

# APP_KEY must be provisioned once by the operator. Never rotate it at startup.
php -r '
require "vendor/autoload.php";
$app = require "bootstrap/app.php";
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();
if (! $app["config"]->get("app.key")) {
    fwrite(STDERR, "APP_KEY ausente: configure uma chave persistente antes de iniciar.\n");
    exit(1);
}
'

# Database installation is explicit; startup does not read/write any database.

exec php artisan serve --host=0.0.0.0 --port=8000
