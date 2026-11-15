<?php

use App\Database\PostgresMigration;

return new class extends PostgresMigration
{
    public function up(): void
    {
        $this->sql('extensions.up');
    }

    public function down(): void
    {
        $this->sql('extensions.down');
    }
};
