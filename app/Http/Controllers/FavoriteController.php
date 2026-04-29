<?php

namespace App\Http\Controllers;

use App\Http\Requests\Favorite\StoreFavoriteRequest;
use App\Repositories\FavoriteRepository;
use Illuminate\Http\JsonResponse;

class FavoriteController extends Controller
{
    public function __construct(
        private readonly FavoriteRepository $favoriteRepository
    ) {}

    /**
     * Lista os favoritos do usuário autenticado.
     */
    public function index(): JsonResponse
    {
        $favorites = $this->favoriteRepository->getByUser(auth()->id());

        return response()->json([
            'message' => 'Favoritos recuperados com sucesso.',
            'data'    => $favorites,
        ]);
    }

    /**
     * Adiciona uma linha aos favoritos do usuário.
     */
    public function store(StoreFavoriteRequest $request): JsonResponse
    {
        $favorite = $this->favoriteRepository->create(
            userId: auth()->id(),
            lineId: $request->validated('line_id'),
        );

        return response()->json([
            'message' => 'Favorito adicionado com sucesso.',
            'data'    => $favorite,
        ], 201);
    }

    /**
     * Remove uma linha dos favoritos do usuário.
     */
    public function destroy(int $id): JsonResponse
    {
        $deleted = $this->favoriteRepository->deleteByUser(
            userId: auth()->id(),
            favoriteId: $id,
        );

        if (!$deleted) {
            return response()->json([
                'message' => 'Favorito não encontrado.',
            ], 404);
        }

        return response()->json([
            'message' => 'Favorito removido com sucesso.',
        ]);
    }
}
