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

    /** A code is only a lookup key; ambiguous operational identities are rejected. */
    public function findByCodeAndDirection(string $code, int $direction): ?Line
    {
        $lines = Line::where('provider', 'sptrans')->where('code', $code)
            ->where('sptrans_direction', $direction)->limit(2)->get();

        return $lines->count() === 1 ? $lines->first() : null;
    }

}
