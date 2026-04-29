<?php

use App\Http\Controllers\AuthController;
use App\Http\Controllers\BusController;
use App\Http\Controllers\FavoriteController;
use App\Http\Controllers\LineController;
use Illuminate\Support\Facades\Route;

/*
|--------------------------------------------------------------------------
| API Routes — Urban Mobility
|--------------------------------------------------------------------------
*/

// Auth (público)
Route::prefix('auth')->group(function () {
    Route::post('/login',    [AuthController::class, 'login']);
    Route::post('/register', [AuthController::class, 'register']);
});

// Rotas protegidas por Sanctum
Route::middleware('auth:sanctum')->group(function () {

    // Auth
    Route::post('/auth/logout', [AuthController::class, 'logout']);

    // Linhas
    Route::get('/lines',      [LineController::class, 'index']);
    Route::get('/lines/search', [LineController::class, 'search']);
    Route::get('/lines/{id}', [LineController::class, 'show']);

    // Posição em tempo real dos ônibus de uma linha
    Route::get('/lines/{lineId}/buses', [BusController::class, 'positions']);

    // Favoritos
    Route::get('/favorites',         [FavoriteController::class, 'index']);
    Route::post('/favorites',        [FavoriteController::class, 'store']);
    Route::delete('/favorites/{id}', [FavoriteController::class, 'destroy']);
});
