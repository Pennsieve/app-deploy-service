package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/app-deploy-service/service/utils"
)

type RouterHandlerFunc func(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)

// Defines the router interface
type Router interface {
	POST(string, RouterHandlerFunc)
	GET(string, RouterHandlerFunc)
	DELETE(string, RouterHandlerFunc)
	PUT(string, RouterHandlerFunc)
	Start(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)
}

type LambdaRouter struct {
	getRoutes    map[string]RouterHandlerFunc
	postRoutes   map[string]RouterHandlerFunc
	deleteRoutes map[string]RouterHandlerFunc
	putRoutes    map[string]RouterHandlerFunc
}

func NewLambdaRouter() Router {
	return &LambdaRouter{
		make(map[string]RouterHandlerFunc),
		make(map[string]RouterHandlerFunc),
		make(map[string]RouterHandlerFunc),
		make(map[string]RouterHandlerFunc),
	}
}

func (r *LambdaRouter) POST(routeKey string, handler RouterHandlerFunc) {
	r.postRoutes[routeKey] = handler
}

func (r *LambdaRouter) GET(routeKey string, handler RouterHandlerFunc) {
	r.getRoutes[routeKey] = handler
}

func (r *LambdaRouter) DELETE(routeKey string, handler RouterHandlerFunc) {
	r.deleteRoutes[routeKey] = handler
}

func (r *LambdaRouter) PUT(routeKey string, handler RouterHandlerFunc) {
	r.putRoutes[routeKey] = handler
}

func (r *LambdaRouter) Start(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	log.Println(request)
	routeKey := utils.ExtractRoute(request.RouteKey)

	var routes map[string]RouterHandlerFunc
	switch request.RequestContext.HTTP.Method {
	case http.MethodPost:
		routes = r.postRoutes
	case http.MethodGet:
		routes = r.getRoutes
	case http.MethodDelete:
		routes = r.deleteRoutes
	case http.MethodPut:
		routes = r.putRoutes
	default:
		log.Println(ErrUnsupportedPath.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       ErrUnsupportedPath.Error(),
		}, nil
	}

	if f, ok := routes[routeKey]; ok {
		return f(ctx, request)
	}

	if routeKey == "$default" {
		return r.matchByPath(ctx, request, routes)
	}

	return handleError()
}

func (r *LambdaRouter) matchByPath(ctx context.Context, request events.APIGatewayV2HTTPRequest, routes map[string]RouterHandlerFunc) (events.APIGatewayV2HTTPResponse, error) {
	path := request.RawPath
	patterns := make([]string, 0, len(routes))
	for p := range routes {
		patterns = append(patterns, p)
	}

	matched, params, ok := utils.MatchRoute(path, patterns)
	if !ok {
		return handleError()
	}

	if request.PathParameters == nil {
		request.PathParameters = params
	} else {
		for k, v := range params {
			request.PathParameters[k] = v
		}
	}

	return routes[matched](ctx, request)
}

func handleError() (events.APIGatewayV2HTTPResponse, error) {
	log.Println(ErrUnsupportedRoute.Error())
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusNotFound,
		Body:       ErrUnsupportedRoute.Error(),
	}, nil
}
