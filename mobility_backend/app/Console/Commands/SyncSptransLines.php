<?php

namespace App\Console\Commands;

use App\Services\LineService;
use Illuminate\Console\Command;

class SyncSptransLines extends Command
{
    /**
     * O nome e a assinatura do comando no console.
     * Ex: php artisan sptrans:sync
     */
    protected $signature = 'sptrans:sync';

    /**
     * A descrição do comando.
     */
    protected $description = 'Sincroniza as linhas de ônibus com a API da SPTrans';

    /**
     * Executa o comando.
     */
    public function handle(LineService $service): void
    {
        $this->info('Iniciando sincronização via serviço Go...');

        $termos = ['0', '1', '2', '3', '4', '5', '6', '7', '8', '9'];

        foreach ($termos as $termo) {
            $this->line("Buscando linhas com termo: {$termo}");
            $service->syncByTerm($termo);
        }

        $total = \App\Models\Line::count();
        $this->info("Concluído! {$total} linhas salvas no banco.");
    }
}
