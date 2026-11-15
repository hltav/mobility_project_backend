<?php

namespace Tests\PostgreSQL;

use App\Models\BusStop;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class HttpCompatibilityTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
        if (getenv('POSTGRES_STAGE1_TESTS') !== '1' || DB::connection()->getDriverName() !== 'pgsql'
            || ! in_array(DB::connection()->getDatabaseName(), ['open_transit_test', 'open_transit_schema_test'], true)) {
            $this->markTestSkipped('Use the isolated PostgreSQL executor.');
        }
        Http::preventStrayRequests();
        DB::beginTransaction();
    }

    protected function tearDown(): void
    {
        while (DB::transactionLevel() > 0) {
            DB::rollBack();
        }
        parent::tearDown();
    }

    private function registration(string $email = 'fixture@example.test'): array
    {
        return ['name' => 'Fixture User', 'email' => $email, 'password' => 'fixture-password', 'password_confirmation' => 'fixture-password'];
    }

    public function test_registration_login_and_token_envelopes_are_preserved(): void
    {
        $response = $this->postJson('/api/auth/register', $this->registration('Mixed.Case@example.test'));
        $response->assertCreated()->assertJsonStructure(['message', 'data' => ['user' => ['id', 'name', 'email'], 'token']]);
        $this->assertArrayNotHasKey('password', $response->json('data.user'));
        $this->assertArrayNotHasKey('remember_token', $response->json('data.user'));
        $this->assertTrue(Hash::check('fixture-password', DB::table('users')->value('password')));
        $token = $response->json('data.token');
        [, $plain] = explode('|', $token, 2);
        $this->assertSame(hash('sha256', $plain), DB::table('personal_access_tokens')->value('token'));
        $this->postJson('/api/auth/login', ['email' => 'mixed.case@example.test', 'password' => 'fixture-password'])
            ->assertOk()->assertJsonStructure(['message', 'data' => ['user' => ['id', 'name', 'email'], 'token']]);
        $this->assertSame(2, DB::table('personal_access_tokens')->count());
        $this->postJson('/api/auth/register', $this->registration('MIXED.CASE@example.test'))->assertUnprocessable()->assertJsonValidationErrors('email');
    }

    public function test_invalid_login_and_unauthenticated_routes_still_reject_requests(): void
    {
        $this->postJson('/api/auth/login', ['email' => 'missing@example.test', 'password' => 'fixture-password'])->assertUnauthorized();
        $this->postJson('/api/auth/register', ['email' => 'invalid'])->assertUnprocessable()->assertJsonValidationErrors(['name', 'email', 'password']);
        $this->getJson('/api/favorites')->assertUnauthorized();
    }

    public function test_favorites_and_line_read_contracts_keep_internal_ids(): void
    {
        $auth = $this->postJson('/api/auth/register', $this->registration())->assertCreated();
        $token = $auth->json('data.token');
        $line = DB::table('public.lines')->insertGetId(['sptrans_line_id' => 12345, 'display_code' => '8000-10', 'display_name' => 'Fixture', 'sptrans_direction' => 1, 'is_circular' => false, 'headsign_primary_to_secondary' => 'Primary', 'headsign_secondary_to_primary' => 'Secondary']);
        $first = $this->withToken($token)->postJson('/api/favorites', ['line_id' => $line]);
        $first->assertCreated()->assertJsonPath('data.line_id', $line);
        $this->withToken($token)->postJson('/api/favorites', ['line_id' => $line])->assertCreated();
        $this->assertSame(1, DB::table('favorites')->count());
        $list = $this->withToken($token)->getJson('/api/favorites')->assertOk();
        $list->assertJsonPath('data.0.line.id', $line)->assertJsonPath('data.0.line.sptrans_id', 12345)->assertJsonPath('data.0.line.code', '8000-10');
        $this->assertArrayNotHasKey('display_code', $list->json('data.0.line'));
        $this->assertArrayNotHasKey('sptrans_direction', $list->json('data.0.line'));
        $this->withToken($token)->deleteJson('/api/favorites/'.$first->json('data.id'))->assertOk();
        $this->assertSame(0, DB::table('favorites')->count());
    }

    public function test_logout_revokes_tokens_and_future_requests_are_unauthorized(): void
    {
        $token = $this->postJson('/api/auth/register', $this->registration())->json('data.token');
        $this->withToken($token)->postJson('/api/auth/logout')->assertOk()->assertJsonStructure(['message']);
        $this->assertSame(0, DB::table('personal_access_tokens')->count());
        // Clear the guard's per-request user cache, as a fresh HTTP request does.
        $this->app['auth']->forgetGuards();
        $this->withToken($token)->getJson('/api/favorites')->assertUnauthorized();
    }

    public function test_registry_keeps_unpublished_stops_hidden_and_uses_confirmed_sptrans_reference(): void
    {
        $id = DB::table('public.bus_stops')->insertGetId(['name' => 'Registry fixture']);
        $this->assertNull(BusStop::find($id));
        DB::table('public.bus_stops')->where('id', $id)->update(['legacy_visible' => true]);
        $this->assertNull(BusStop::findOrFail($id)->sptrans_code);
        DB::table('public.stop_source_references')->insert(['stop_id' => $id, 'provider' => 'sptrans', 'source_kind' => 'sptrans', 'sptrans_stop_code' => 123, 'external_stop_id' => '123', 'status' => 'pending', 'method' => 'fixture']);
        $this->assertNull(BusStop::findOrFail($id)->sptrans_code);
        DB::table('public.stop_source_references')->where('stop_id', $id)->update(['status' => 'confirmed']);
        $this->assertSame(123, BusStop::findOrFail($id)->sptrans_code);
        $this->assertArrayNotHasKey('position_geom',BusStop::findOrFail($id)->toArray());
    }
}
