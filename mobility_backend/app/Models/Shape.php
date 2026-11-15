<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class Shape extends Model
{
    protected $fillable = [
        'shape_id',
        'latitude',
        'longitude',
        'sequence',
        'distance_traveled',
    ];

    protected $casts = [
        'latitude' => 'float',
        'longitude' => 'float',
        'distance_traveled' => 'float',
    ];
}
