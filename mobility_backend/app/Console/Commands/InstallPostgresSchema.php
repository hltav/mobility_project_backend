<?php

namespace App\Console\Commands;

use Illuminate\Console\Command;
use Illuminate\Support\Facades\DB;

class InstallPostgresSchema extends Command
{
    protected $signature = 'schema:install-postgresql {--force : Confirm explicit installation on the configured PostgreSQL database}';

    protected $description = 'Install only the canonical PostgreSQL migrations (never the historical MySQL path)';

    public function handle(): int
    {
        if (! $this->option('force')) {
            $this->error('Use --force after checking PGSQL_HOST and PGSQL_DATABASE. No migration executed.');

            return self::FAILURE;
        }

        $db = DB::connection('pgsql');
        if ($db->getDriverName() !== 'pgsql') {
            $this->error('The pgsql connection must use the PostgreSQL driver.');

            return self::FAILURE;
        }

        $identity = $db->selectOne('SELECT current_database() AS database, current_user AS username');
        $this->line("Target PostgreSQL: {$identity->database}; role: {$identity->username}");
        if ($db->selectOne("SELECT to_regclass('public.migrations') AS name")->name !== null) {
            $unexpected = $db->table('public.migrations')->where('migration', 'not like', '2026_10_25_%')->exists();
            if ($unexpected) {
                $this->error('Historical migrations detected. This command requires the independent canonical baseline.');

                return self::FAILURE;
            }
        }

        return $this->call('migrate', [
            '--database' => 'pgsql',
            '--path' => 'database/postgresql',
            '--force' => true,
        ]);
    }
}
