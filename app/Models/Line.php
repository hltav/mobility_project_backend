<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsToMany;
use Illuminate\Database\Eloquent\Relations\HasMany;

class Line extends Model
{
    use HasFactory;

    protected $fillable = [
        'code',           // Ex: 8000-10
        'name',           // Ex: Term. Pq. Dom Pedro II
        'origin',
        'destination',
        'circular',       // bool - linha circular?
        'sptrans_id',     // ID interno da SPTrans
        'origin_lat',
        'origin_lng',
    ];

    protected $casts = [
        'circular' => 'boolean',
        'sptrans_id' => 'integer',
        'origin_lat' => 'float',
        'origin_lng' => 'float',
    ];

    protected $hidden = [
        'created_at',
        'updated_at',
    ];

    public function favorites(): HasMany
    {
        return $this->hasMany(Favorite::class);
    }

    public function buses(): HasMany
    {
        return $this->hasMany(Bus::class);
    }

    public function busStops(): BelongsToMany
    {
        return $this->belongsToMany(BusStop::class, 'bus_stop_line');
    }
}
