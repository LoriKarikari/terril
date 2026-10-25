<?php

use Illuminate\Support\Facades\Route;

Route::get('/', function () {
    return view('welcome');
});

Route::get('/hello', function () {
    $manifest = file_exists(public_path('build/manifest.json')) ? 'yes' : 'no';

    return response('hello from laravel '.app()->version().' on php '.PHP_VERSION.', vite manifest: '.$manifest)
        ->header('Content-Type', 'text/plain');
});
