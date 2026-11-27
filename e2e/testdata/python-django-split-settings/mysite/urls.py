from django.conf import settings
from django.http import HttpResponse
from django.urls import path


def index(request):
    return HttpResponse((settings.STATIC_ROOT / "app.js").read_text())


urlpatterns = [path("", index)]
