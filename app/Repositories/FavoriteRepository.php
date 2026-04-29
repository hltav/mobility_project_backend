<?php

namespace App\Repositories;

use App\Models\Favorite;
use Illuminate\Database\Eloquent\Collection;

class FavoriteRepository
{
    /**
     * Retorna todos os favoritos de um usuário com a linha relacionada.
     */
    public function getByUser(int $userId): Collection
    {
        return Favorite::with('line')
            ->where('user_id', $userId)
            ->latest()
            ->get();
    }

    /**
     * Adiciona uma linha aos favoritos do usuário.
     * Garante unicidade (sem duplicata).
     */
    public function create(int $userId, int $lineId): Favorite
    {
        return Favorite::firstOrCreate([
            'user_id' => $userId,
            'line_id' => $lineId,
        ]);
    }

    /**
     * Remove um favorito do usuário (valida ownership).
     */
    public function deleteByUser(int $userId, int $favoriteId): bool
    {
        return Favorite::where('id', $favoriteId)
            ->where('user_id', $userId)
            ->delete() > 0;
    }
}
