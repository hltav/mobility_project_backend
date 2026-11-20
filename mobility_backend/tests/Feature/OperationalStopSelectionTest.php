<?php

namespace Tests\Feature;

use App\Models\Line;
use App\Models\User;
use App\Repositories\LineRepository;
use App\Repositories\OperationalStopRepository;
use App\Services\BusStopService;
use App\Services\ProcessorService;
use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Log;
use Laravel\Sanctum\Sanctum;
use Tests\TestCase;

class OperationalStopSelectionTest extends TestCase
{
    public function test_gps_response_only_contains_public_stop_coordinates(): void
    {
        Sanctum::actingAs(new User);
        $this->mock(BusStopService::class)->shouldReceive('selectOperational')->once()->andReturn([
            'status' => 'available', 'stops' => [['stop_id' => 7, 'lat' => -23.55, 'lng' => -46.63, 'passenger_distance_m' => 120]],
        ]);
        $response = $this->postJson('/api/v1/stops/select', ['operational_line_id' => 1, 'lat' => -23.551, 'lng' => -46.631]);
        $response->assertOk()->assertJsonMissingPath('data.point')->assertJsonPath('data.stops.0.lat', -23.55);
        $this->assertStringNotContainsString('-23.551', $response->getContent());
        $this->assertStringNotContainsString('-46.631', $response->getContent());
    }

    public function test_post_supports_manual_selection_and_rejects_missing_or_invalid_gps(): void
    {
        Sanctum::actingAs(new User);
        $this->mock(BusStopService::class)->shouldReceive('selectOperational')->once()
            ->with(['operational_line_id' => 1, 'stop_id' => 7])
            ->andReturn(['status' => 'available', 'stops' => [['stop_id' => 7, 'passenger_distance_m' => null]]]);
        $this->postJson('/api/v1/stops/select', ['operational_line_id' => 1, 'stop_id' => 7])
            ->assertOk()->assertHeader('Cache-Control', 'no-store, private')->assertJsonMissingPath('data.point');
        foreach ([['operational_line_id' => 1], ['operational_line_id' => 1, 'lat' => 91, 'lng' => 0],
            ['operational_line_id' => 1, 'stop_id' => 7, 'lat' => 0]] as $invalid) {
            $this->postJson('/api/v1/stops/select', $invalid)->assertUnprocessable()->assertHeader('Cache-Control', 'no-store, private');
        }
        $this->postJson('/api/v1/stops/select?lat=-23.551', ['operational_line_id' => 1, 'stop_id' => 7])->assertUnprocessable();
    }

    public function test_line_identity_is_checked_without_switching_to_the_opposite_direction(): void
    {
        $line = new Line;
        $line->setRawAttributes(['id' => 1, 'provider' => 'sptrans', 'sptrans_id' => 2506, 'sptrans_direction' => 1]);
        $lines = $this->createMock(LineRepository::class); $lines->method('findById')->willReturn($line);
        $stops = $this->createMock(OperationalStopRepository::class);
        $stops->expects($this->once())->method('select')->with(1, 2506, null, null, 7, 2.0, 5)
            ->willReturn(['status' => 'unavailable', 'reason' => 'no_confirmed_stop_association', 'stops' => []]);
        $service = new BusStopService($this->createMock(ProcessorService::class), $lines, $stops);
        $this->assertSame('operational_identity_mismatch', $service->selectOperational([
            'operational_line_id' => 1, 'stop_id' => 7, 'sptrans_direction' => 2])['reason']);
        $this->assertSame('operational_identity_mismatch', $service->selectOperational([
            'operational_line_id' => 1, 'stop_id' => 7, 'sptrans_line_id' => 35274])['reason']);
        $this->assertSame('no_confirmed_stop_association', $service->selectOperational([
            'operational_line_id' => 1, 'stop_id' => 7])['reason']);
    }

    public function test_selection_errors_do_not_log_sql_bindings_or_personal_coordinates(): void
    {
        Sanctum::actingAs(new User);
        $this->mock(BusStopService::class)->shouldReceive('selectOperational')->andThrow(new \RuntimeException('SQL with -23.551 -46.631'));
        Log::shouldReceive('error')->once()->with('[Stops] operational selection failed', ['exception_type' => \RuntimeException::class]);
        $response = $this->postJson('/api/v1/stops/select', ['operational_line_id' => 1, 'lat' => -23.551, 'lng' => -46.631]);
        $response->assertStatus(503)->assertHeader('Cache-Control', 'no-store, private')->assertJsonMissingPath('data.point');
        $this->assertStringNotContainsString('-23.551', $response->getContent());
    }

    public function test_legacy_get_shape_remains_and_processor_errors_have_sanitized_logs(): void
    {
        Sanctum::actingAs(new User);
        Http::preventStrayRequests();
        Http::fake(['*' => Http::response(['sensitive' => '-23.551'], 500)]);
        Log::shouldReceive('error')->once()->with('[Processor] getNearestStops retornou HTTP inválido', ['status' => 500]);
        $this->getJson('/api/stops/nearest?lat=-23.551&lng=-46.631')->assertOk()
            ->assertHeader('Cache-Control', 'no-store, private')->assertJsonPath('data.point.lat', -23.551)->assertJsonPath('data.stops', []);
    }
}
