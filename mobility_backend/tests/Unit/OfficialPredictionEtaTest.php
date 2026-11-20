<?php

namespace Tests\Unit;

use App\Services\OfficialPredictionEta;
use Carbon\CarbonImmutable;
use PHPUnit\Framework\TestCase;

class OfficialPredictionEtaTest extends TestCase
{
    private function snapshot(): array
    {
        // Synthetic contract fixture, not a homologated SPTrans payload.
        return ['schema_version'=>1,'source'=>'sptrans','sptrans_line_id'=>2506,'sptrans_direction'=>1,
            'status'=>'available','fetched_at'=>'2026-11-20T03:00:16Z','valid_until'=>'2026-11-20T03:00:36Z',
            'predictions'=>['hr'=>'00:00','ps'=>[['cp'=>700016623,'vs'=>[
                ['p'=>'12601','t'=>'00:02','ta'=>'2026-11-20T03:00:15Z']]]]]];
    }

    private function eta(?array $snapshot): array
    {
        return (new OfficialPredictionEta)->forStop($snapshot,2506,1,'12601',700016623,
            CarbonImmutable::parse('2026-11-20T03:00:20Z'));
    }

    public function test_fresh_official_clock_yields_minutes_in_sao_paulo(): void
    {
        $out=$this->eta($this->snapshot());
        $this->assertSame('available',$out['status']);
        $this->assertSame(100,$out['eta_seconds']);
        $this->assertSame(2,$out['eta_minutes']);
        $this->assertSame('2026-11-20T00:02:00-03:00',$out['predicted_arrival_at']);
    }

    public function test_expiration_uses_semantics_and_twenty_second_cap_not_physical_ttl(): void
    {
        $s=$this->snapshot(); $s['valid_until']='2026-11-20T03:00:20Z';
        $this->assertSame('unavailable',$this->eta($s)['status']);
        $s=$this->snapshot(); $s['fetched_at']='2026-11-20T02:59:59Z'; $s['valid_until']='2026-11-20T03:02:00Z';
        $this->assertSame('unavailable',$this->eta($s)['status']);
    }

    public function test_cl_direction_cp_prefix_and_duplicate_prediction_fail_closed(): void
    {
        foreach (['sptrans_line_id'=>35274,'sptrans_direction'=>2] as $key=>$value) {
            $s=$this->snapshot(); $s[$key]=$value;
            $this->assertNull($this->eta($s)['eta_minutes']);
        }
        $s=$this->snapshot(); $s['predictions']['ps'][0]['cp']=123;
        $this->assertNull($this->eta($s)['eta_minutes']);
        $s=$this->snapshot(); $s['predictions']['ps'][0]['vs'][0]['p']='012601';
        $this->assertNull($this->eta($s)['eta_minutes']);
        $s=$this->snapshot(); $s['predictions']['ps'][0]['vs'][]=$s['predictions']['ps'][0]['vs'][0];
        $this->assertNull($this->eta($s)['eta_minutes']);
    }

    public function test_midnight_uses_dated_capture_and_reference_clock(): void
    {
        $s=$this->snapshot(); $s['fetched_at']='2026-11-20T02:59:50Z'; $s['valid_until']='2026-11-20T03:00:10Z';
        $s['predictions']['hr']='23:59'; $s['predictions']['ps'][0]['vs'][0]['ta']='2026-11-20T02:59:49Z';
        $out=(new OfficialPredictionEta)->forStop($s,2506,1,'12601',700016623,CarbonImmutable::parse('2026-11-20T02:59:55Z'));
        $this->assertSame('available',$out['status']);
        $this->assertSame('2026-11-20T00:02:00-03:00',$out['predicted_arrival_at']);
        $this->assertSame(3,$out['eta_minutes']);
    }

    public function test_missing_invalid_and_unproved_times_never_invent_dates(): void
    {
        $this->assertNull($this->eta(null)['eta_minutes']);
        foreach ([null,'00:00','invalid','2026-02-30T03:00:15Z'] as $capture) {
            $s=$this->snapshot(); $s['predictions']['ps'][0]['vs'][0]['ta']=$capture;
            $this->assertNull($this->eta($s)['eta_minutes']);
        }
        $s=$this->snapshot(); $s['predictions']['hr']='12:00';
        $this->assertNull($this->eta($s)['eta_minutes']);
        $s=$this->snapshot(); $s['predictions']['ps'][0]['vs'][0]['t']='00:00';
        $this->assertNull($this->eta($s)['eta_minutes']);
    }
}
