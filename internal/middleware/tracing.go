package middleware

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware creates a middleware that instruments HTTP requests with OpenTelemetry tracing.
func TracingMiddleware(next http.Handler) http.Handler {
	tracer := otel.Tracer("http-server")
	propagator := propagation.TraceContext{}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := propagator.Extract(req.Context(), propagation.HeaderCarrier(req.Header))
		ctx, span := tracer.Start(ctx, req.URL.Path, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		span.SetAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.target", req.URL.Path),
			attribute.String("http.flavor", req.Proto),
			attribute.String("http.user_agent", req.UserAgent()),
			attribute.String("http.client_ip", req.RemoteAddr),
		)

		req = req.WithContext(ctx)
		next.ServeHTTP(w, req)

		// TODO: Set status code and other response attributes after handler execution
	})
}
