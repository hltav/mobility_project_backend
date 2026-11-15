<?php

use App\Database\PostgresMigration;

return new class extends PostgresMigration
{
    public function up(): void
    {
        $this->sql('ingest.up');
    }

    public function down(): void
    {
        $this->sql('ingest.down');
    }
};
