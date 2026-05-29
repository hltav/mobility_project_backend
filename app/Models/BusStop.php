<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsToMany;

class BusStop extends Model
{
    use HasFactory;

    protected $fillable = [
        'sptrans_code',
        'name',
        'address',
        'latitude',
        'longitude',
    ];

    protected $casts = [
        'sptrans_code' => 'integer',
        'latitude' => 'float',
        'longitude' => 'float',
    ];

    protected $hidden = [
        'created_at',
        'updated_at',
        'pivot',
    ];

    public function lines(): BelongsToMany
    {
        return $this->belongsToMany(Line::class, 'bus_stop_line');
    }
}
