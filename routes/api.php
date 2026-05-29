<?php

use App\Http\Controllers\AuthController;
use App\Http\Controllers\BusController;
use App\Http\Controllers\BusStopController;
use App\Http\Controllers\FavoriteController;
use App\Http\Controllers\LineController;
use Illuminate\Support\Facades\Route;
use App\Http\Controllers\GeocodingController;

// Auth (público)
Route::prefix('auth')->group(function () {
    Route::post('/login',    [AuthController::class, 'login']);
    Route::post('/register', [AuthController::class, 'register']);
});

// Geocodificação
Route::post('/geocode', [GeocodingController::class, 'geocode']);
Route::get('/geocode/reverse', [GeocodingController::class, 'reverse']); // novo
Route::prefix('internal')->group(function () {
    Route::get('/lines/without-coordinates', [LineController::class, 'withoutCoordinates']);
    Route::post('/lines/update-coordinates',  [LineController::class, 'updateCoordinates']);
});

// Rotas protegidas por Sanctum
Route::middleware('auth:sanctum')->group(function () {

    // Auth
    Route::post('/auth/logout', [AuthController::class, 'logout']);


    // Linhas — rotas estáticas SEMPRE antes das dinâmicas
    Route::get('/lines/nearby', [LineController::class, 'nearby']);
    Route::get('/lines/search', [LineController::class, 'search']);
    Route::get('/lines/{id}/stops', [LineController::class, 'stops']);
    Route::get('/lines/{lineId}/buses', [BusController::class, 'positions']);
    Route::get('/lines/{id}',   [LineController::class, 'show']);
    Route::get('/lines',        [LineController::class, 'index']);

    // Pontos de ônibus
    Route::get('/stops/nearest', [BusStopController::class, 'nearest']);
    Route::get('/stops/trip-nearest', [BusStopController::class, 'tripNearest']);

    // Favoritos
    Route::get('/favorites',         [FavoriteController::class, 'index']);
    Route::post('/favorites',        [FavoriteController::class, 'store']);
    Route::delete('/favorites/{id}', [FavoriteController::class, 'destroy']);
});
