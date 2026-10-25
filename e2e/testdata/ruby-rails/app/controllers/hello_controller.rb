require "pg"

class HelloController < ApplicationController
  def index
    precompiled = Rails.public_path.join("assets/.manifest.json").exist?
    render plain: "hello from rails with libpq #{PG.library_version}, assets precompiled: #{precompiled}"
  end
end
