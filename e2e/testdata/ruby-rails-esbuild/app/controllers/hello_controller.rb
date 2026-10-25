class HelloController < ApplicationController
  def index
    bundle = Rails.public_path.glob("assets/application-*.js").first
    render plain: bundle ? "#{bundle.basename}: #{bundle.read.strip}" : "no digested application.js"
  end
end
