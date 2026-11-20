<?php

namespace Tests\Feature;

use App\Services\ValkeyService;
use Illuminate\Support\Facades\Redis;
use Tests\TestCase;

class ValkeyPredictionSnapshotTest extends TestCase
{
    public function test_exact_operational_key_and_invalid_cache_fail_closed(): void
    {
        Redis::shouldReceive('get')->once()->with('sptrans:prediction:v1:cl:2506:sl:1')
            ->andReturn('{invalid');
        $this->assertNull((new ValkeyService)->getPredictionSnapshot(2506,1));
    }

    public function test_missing_snapshot_and_valkey_failure_return_null(): void
    {
        Redis::shouldReceive('get')->once()->with('sptrans:prediction:v1:cl:2506:sl:1')->andReturn(null);
        Redis::shouldReceive('get')->once()->with('sptrans:prediction:v1:cl:35274:sl:2')
            ->andThrow(new \RuntimeException('synthetic cache unavailable'));
        $service=new ValkeyService;
        $this->assertNull($service->getPredictionSnapshot(2506,1));
        $this->assertNull($service->getPredictionSnapshot(35274,2));
    }

    public function test_invalid_identity_never_reads_cache(): void
    {
        Redis::shouldReceive('get')->never();
        $service=new ValkeyService;
        $this->assertNull($service->getPredictionSnapshot(0,1));
        $this->assertNull($service->getPredictionSnapshot(2506,0));
    }
}
