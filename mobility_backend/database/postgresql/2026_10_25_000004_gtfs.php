<?php

use App\Database\PostgresMigration;

return new class extends PostgresMigration
{
    public function up(): void
    {
        $this->sql('gtfs.up');
    }

    public function down(): void
    {
        $this->sql('gtfs.down');
    }
};
