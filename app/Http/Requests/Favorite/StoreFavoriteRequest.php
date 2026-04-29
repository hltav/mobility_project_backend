<?php

namespace App\Http\Requests\Favorite;

use Illuminate\Foundation\Http\FormRequest;

class StoreFavoriteRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'line_id' => ['required', 'integer', 'exists:lines,id'],
        ];
    }

    public function messages(): array
    {
        return [
            'line_id.required' => 'O ID da linha é obrigatório.',
            'line_id.integer'  => 'O ID da linha deve ser um número inteiro.',
            'line_id.exists'   => 'A linha informada não existe.',
        ];
    }
}
