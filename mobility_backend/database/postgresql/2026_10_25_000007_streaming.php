<?php

use App\Database\PostgresMigration;

return new class extends PostgresMigration
{
    public function up(): void
    {
        $this->sql('streaming.up');
    }

    public function down(): void
    {
        $this->sql('streaming.down');
    }
};
