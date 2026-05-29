<?php

namespace App\Repositories;

use App\Models\Line;
use Illuminate\Pagination\LengthAwarePaginator;

class LineRepository
{
    /**
     * Busca linhas com filtro opcional por código ou nome.
     */
    public function search(?string $term, int $perPage = 15): LengthAwarePaginator
    {
        return Line::query()
            ->when($term, fn($q) => $q->where(function ($q) use ($term) {
                $q->where('code', 'like', "%{$term}%")
                    ->orWhere('name', 'like', "%{$term}%")
                    ->orWhere('origin', 'like', "%{$term}%")
                    ->orWhere('destination', 'like', "%{$term}%");
            }))
            ->orderBy('code')
            ->paginate($perPage);
    }

    /**
     * Busca uma linha pelo ID interno.
     */
    public function findById(int $id): ?Line
    {
        return Line::find($id);
    }

    /**
     * Busca uma linha pelo código SPTrans.
     */
    public function findBySptransId(int $sptransId): ?Line
    {
        return Line::where('sptrans_id', $sptransId)->first();
    }

    /**
     * Cria ou atualiza uma linha (usado no sync com SPTrans).
     */
    public function upsert(array $data): Line
    {
        return Line::updateOrCreate(
            ['sptrans_id' => $data['sptrans_id']],
            $data,
        );
    }

    /**
     * Retorna todas as linhas que possuem coordenadas de origem preenchidas.
     * Usado pelo Haversine para filtrar por raio.
     */
    public function allWithCoordinates(): \Illuminate\Support\Collection
    {
        return Line::query()
            ->whereNotNull('origin_lat')
            ->whereNotNull('origin_lng')
            ->get();
    }
}
