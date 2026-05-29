<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::create('bus_stop_line', function (Blueprint $table) {
            $table->id();
            $table->foreignId('bus_stop_id')->constrained('bus_stops')->cascadeOnDelete();
            $table->foreignId('line_id')->constrained('lines')->cascadeOnDelete();
            $table->timestamps();

            $table->unique(['bus_stop_id', 'line_id']);
            $table->index(['line_id', 'bus_stop_id']);
        });
    }

    public function down(): void
    {
        Schema::dropIfExists('bus_stop_line');
    }
};
