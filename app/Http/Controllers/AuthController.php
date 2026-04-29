<?php

namespace App\Http\Controllers;

use App\Http\Requests\Auth\LoginRequest;
use App\Http\Requests\Auth\RegisterRequest;
use App\Http\Requests\GoogleLoginRequest;
use App\Services\AuthService;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Laravel\Socialite\Facades\Socialite;
use App\Models\User;

class AuthController extends Controller
{
    public function __construct(
        private readonly AuthService $authService
    ) {}

    public function login(LoginRequest $request): JsonResponse
    {
        $result = $this->authService->login($request->validated());

        if (!$result) {
            return response()->json([
                'message' => 'Credenciais inválidas.',
            ], 401);
        }

        return response()->json([
            'message' => 'Login realizado com sucesso.',
            'data'    => $result,
        ]);
    }

    public function register(RegisterRequest $request): JsonResponse
    {
        $result = $this->authService->register($request->validated());

        return response()->json([
            'message' => 'Usuário criado com sucesso.',
            'data'    => $result,
        ], 201);
    }

    /**
     * OAuth Google (stateless API)
     */
    public function google(GoogleLoginRequest $request): JsonResponse
    {

        $googleUser = Socialite::driver('google')
            ->stateless()
            ->userFromToken($request->validated()['token']);

        $result = $this->authService->loginWithGoogle($googleUser);

        return response()->json([
            'message' => 'Login com Google realizado com sucesso.',
            'data'    => $result,
        ]);
    }

    public function logout(Request $request): JsonResponse
    {
        /** @var User $user */
        $user = $request->user();

        $this->authService->logout($user);

        return response()->json([
            'message' => 'Logout realizado com sucesso.',
        ]);
    }
}
