<?php

namespace App\Console\Commands;

use App\Models\Line;
use App\Services\ProcessorService;
use Illuminate\Console\Command;

class GeocodeLines extends Command
{
    protected $signature = 'lines:geocode {--fresh : Regeocodifica todas, mesmo as que já têm coordenadas}';
    protected $description = 'Geocodifica as origens das linhas de ônibus';

    public function handle(ProcessorService $processor): int
    {
        $this->info("Solicitando processamento de Geocode ao serviço Go...");

        // Exemplo: O Go pode rodar isso em background via Worker ou API.
        // $goClient->triggerGeocode($this->option('fresh'));

        $this->info("Processamento iniciado no background.");

        return self::SUCCESS;
    }
}
