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
        ];
    }

    public function messages(): array
    {
        return [
            'term.max'       => 'O termo de busca deve ter no máximo 100 caracteres.',
            'per_page.min'   => 'O número de itens por página deve ser ao menos 1.',
            'per_page.max'   => 'O número de itens por página não pode exceder 100.',
        ];
    }
}
