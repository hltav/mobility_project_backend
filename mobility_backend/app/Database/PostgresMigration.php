<?php

namespace App\Database;

use Illuminate\Database\Migrations\Migration;
use Illuminate\Support\Facades\DB;

abstract class PostgresMigration extends Migration
{
    protected $connection = 'pgsql';

    protected function sql(string $name): void
    {
        $db = DB::connection($this->getConnection());
        if ($db->getDriverName() !== 'pgsql') {
            throw new \RuntimeException('Canonical schema requires PostgreSQL.');
        }
        $db->unprepared(file_get_contents(database_path('postgresql/sql/'.$name.'.sql')));
    }
}
