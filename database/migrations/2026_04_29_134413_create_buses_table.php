<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        Schema::create('buses', function (Blueprint $table) {
            $table->id();

            $table->foreignId('line_id')
                ->constrained()
                ->cascadeOnDelete();

            $table->string('prefix')->index(); // identificação do veículo

            $table->boolean('accessible')->default(false);

            $table->decimal('latitude', 10, 7);
            $table->decimal('longitude', 10, 7);

            // última atualização da posição
            $table->timestamp('position_updated_at')->nullable();

            $table->timestamps();
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('buses');
    }
};
