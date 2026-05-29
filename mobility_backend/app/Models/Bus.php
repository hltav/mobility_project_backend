<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class Bus extends Model
{
    use HasFactory;

    protected $fillable = [
        'line_id',
        'prefix',       // Prefixo do veículo (SPTrans)
        'accessible',   // Acessível para PCD?
        'latitude',
        'longitude',
        'updated_at',   // Última atualização de posição
    ];

    protected $casts = [
        'accessible' => 'boolean',
        'latitude'   => 'float',
        'longitude'  => 'float',
    ];

    public function line(): BelongsTo
    {
        return $this->belongsTo(Line::class);
    }
}
