<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class Frequency extends Model
{
    protected $fillable = [
        'trip_id',
        'start_time',
        'end_time',
        'headway_secs',
    ];
}
