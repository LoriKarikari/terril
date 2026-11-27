using System.Globalization;

var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();

app.MapGet("/", () => "hello from .NET " + Environment.Version + ", " +
    new DateTime(2026, 1, 1).ToString("D", new CultureInfo("de-DE")));

app.Run();
