<?php

namespace App\Http\Requests\Line;

use Illuminate\Foundation\Http\FormRequest;

class SearchLineRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'term'     => ['nullable', 'string', 'max:100'],
            'per_page' => ['nullable', 'integer', 'min:1', 'max:100'],

            // campos para busca por proximidade
            'address'   => ['nullable', 'string', 'max:200'],
            'radius_km' => ['nullable', 'numeric', 'min:0.1', 'max:50'],
        ];
    }

    public function messages(): array
    {
        return [
            'term.max'       => 'O termo de busca deve ter no máximo 100 caracteres.',
            'per_page.min'   => 'O número de itens por página deve ser ao menos 1.',
            'per_page.max'   => 'O número de itens por página não pode exceder 100.',
            'address.max'    => 'O endereço de busca deve ter no máximo 200 caracteres.',
            'radius_km.min'  => 'O raio de busca deve ser ao menos 0.1 km.',
            'radius_km.max'  => 'O raio de busca não pode exceder 50 km.',
        ];
    }
}
