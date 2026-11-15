<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class Bus extends Model
{
    use HasFactory;

    protected $fillable = [
        'sptrans_line_id',
        'prefix',
        'accessible',
        'latitude',
        'longitude',
        'last_position_at',
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
