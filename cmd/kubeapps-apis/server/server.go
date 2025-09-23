// Copyright 2021-2023 the Kubeapps contributors.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	grpchealth "github.com/bufbuild/connect-grpchealth-go"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/vmware-tanzu/kubeapps/cmd/kubeapps-apis/core"
	packagesv1alpha1 "github.com/vmware-tanzu/kubeapps/cmd/kubeapps-apis/core/packages/v1alpha1"
	pluginsv1alpha1 "github.com/vmware-tanzu/kubeapps/cmd/kubeapps-apis/core/plugins/v1alpha1"
	packagesGRPCv1alpha1 "github.com/vmware-tanzu/kubeapps/cmd/kubeapps-apis/gen/core/packages/v1alpha1"
	packagesConnect "github.com/vmware-tanzu/kubeapps/cmd/kubeapps-apis/gen/core/packages/v1alpha1/v1alpha1connect"
	pluginsGRPCv1alpha1 "github.com/vmware-tanzu/kubeapps/cmd/kubeapps-apis/gen/core/plugins/v1alpha1"
	pluginsConnect "github.com/vmware-tanzu/kubeapps/cmd/kubeapps-apis/gen/core/plugins/v1alpha1/v1alpha1connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	log "k8s.io/klog/v2"
	"github.com/bufbuild/connect-go"
	"bytes"
    "encoding/json"
)

// 환경변수
type saTokenInterceptor struct{
    openApiHost  string
    saNamespace  string
    saName       string
}

// open-api-k8s API 호출로 Kubeapps admin sa token 발급받기
func getSATokenFromAPI(openApiHost, saNamespace, saName string) (string, error) {
    url := fmt.Sprintf("http://%s/k8s/api/v1/clusters/default/namespaces/%s/serviceaccounts/%s/token", openApiHost, saNamespace, saName)

    requestBody := map[string]interface{}{
        "apiVersion": "authentication.k8s.io/v1",
        "kind":       "TokenRequest",
        "metadata": map[string]string{
            "name":      saName,
            "namespace": saNamespace,
        },
        "spec": map[string]interface{}{
            "audiences":         []string{"kubernetes"},
            "expirationSeconds": 3600,
        },
    }

	// JSON 형식으로 변환
    bodyBytes, err := json.Marshal(requestBody)
    if err != nil {
        return "", err
    }

    // HTTP POST 요청 전송
    resp, err := http.Post(url, "application/json", bytes.NewBuffer(bodyBytes))
    if err != nil {
        return "", err
    }

	// 함수 종료 시 응답 Body 닫기
    defer resp.Body.Close()

	// HTTP 응답 상태 코드 확인
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("failed to get SA token, status: %s", resp.Status)
    }

	// 응답 JSON 파싱용 구조체 정의
	var response struct {
		Status struct {
			Token string `json:"token"`
		} `json:"status"`
	}

    // 응답 body를 JSON으로 디코딩 
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}

	// 발급받은 토큰 반환
	return response.Status.Token, nil

}



// sa token 인터셉터 추가
// updated at: 250923
// updated by: 이호형
// Interceptor struct 방식으로 변경
// const adminSAToken = "eyJhbGciOiJSUzI1NiIsImtpZCI6ImplcktWSnRndDc3Y2l1VXUwSVk5SVBKMXBaMlRIdjRzanRkYTM5V3QxZTQifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJrdWJlYXBwcyIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VjcmV0Lm5hbWUiOiJrdWJlYXBwcy1hZG1pbi10b2tlbiIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUiOiJrdWJlYXBwcy1hZG1pbiIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjAwZTA4Zjc1LTYxYjUtNDUxMy1iOGNlLWU3YjlhYTc2MTIyMiIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDprdWJlYXBwczprdWJlYXBwcy1hZG1pbiJ9.cTyPEiMF5tF7F_3PW9r5FoTqChHKIONS3GTwKlMCBcXu2ePRS54-5wnZebAQmpEvsWNoqU8K_qSFK_609B90St-K2bZTReQXLe10FigtDXURXmNaPiLnY-ItDccEVxUJ4wKDhAQ731U9_c_Xs4alghB2RfhMELo8P_6DGH92RK6eMtrELyMvayQ3b-HDzscNTaixJHAwf59_wHNR2xjibWZXvG_LK3sVo5r7p19crMASERH1QNjl7CSMBJLYgOtQc6817uGZ5RNLwlUgvHTfC7XtY18uLCcHhVcWY5oBRjs8PV0IGd4uE80y_-h8NXyhKaWAqislNn9ZlgqP36udww"

// type saTokenInterceptor struct{}

func (i *saTokenInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
    return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
        method := req.Spec().Procedure
        if strings.Contains(method, "CreateInstalledPackage") ||
           strings.Contains(method, "UpdateInstalledPackage") ||
           strings.Contains(method, "DeleteInstalledPackage") {

			// SA 토큰 가져오기
            adminSAToken, err := getSATokenFromAPI(i.openApiHost, i.saNamespace, i.saName)
            if err != nil {
                return nil, fmt.Errorf("failed to get SA token: %v", err)
            }

            req.Header().Set("Authorization", "Bearer "+adminSAToken)
        }
        return next(ctx, req)
    }
}

func (i *saTokenInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
    return next
}

func (i *saTokenInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
    return next
}


func getLogLevelOfEndpoint(endpoint string) log.Level {

	// Add all endpoint function names which you want to suppress in interceptor logging
	suppressLoggingOfEndpoints := []string{"GetConfiguredPlugins"}
	var level log.Level

	// level=3 is default logging level
	level = 3
	for i := 0; i < len(suppressLoggingOfEndpoints); i++ {
		if strings.Contains(endpoint, suppressLoggingOfEndpoints[i]) {
			level = 4
			break
		}
	}

	return level
}

// LogRequest is a gRPC UnaryServerInterceptor that will log the API call
func LogRequest(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response interface{}, err error) {

	start := time.Now()
	res, err := handler(ctx, req)

	level := getLogLevelOfEndpoint(info.FullMethod)

	// Format string : [status code] [duration] [full path]
	// OK 97.752µs /kubeappsapis.core.packages.v1alpha1.PackagesService/GetAvailablePackageSummaries
	log.V(level).Infof("%v %s %s\n",
		status.Code(err),
		time.Since(start),
		info.FullMethod)

	return res, err
}

// Serve is the root command that is run when no other sub-commands are present.
// It runs the gRPC service, registering the configured plugins.
func Serve(serveOpts core.ServeOptions) error {
	listenAddr := fmt.Sprintf(":%d", serveOpts.Port)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gw, err := gatewayMux()
	if err != nil {
		return fmt.Errorf("failed to create gRPC gateway: %w", err)
	}

	// Note: we point the gateway at our *new* gRPC handler, so that we can continue to use
	// the gateway for a ReST-ish API
	gwArgs := core.GatewayHandlerArgs{
		Ctx:         ctx,
		Mux:         gw,
		Addr:        listenAddr,
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	}

	mux := http.NewServeMux()

	// Create the core.plugins.v1alpha1 server which handles registration of
	// plugins, and register it for both grpc and http.
	pluginsServer, err := pluginsv1alpha1.NewPluginsServer(serveOpts, gwArgs, mux)
	if err != nil {
		return fmt.Errorf("failed to initialize plugins server: %v", err)
	}
	if err := registerPluginsServiceServer(mux, pluginsServer, gwArgs); err != nil {
		return fmt.Errorf("failed to register plugins server: %v", err)
	}
	if err := registerPackagesServiceServer(mux, pluginsServer, gwArgs); err != nil {
		return err
	}
	if err := registerRepositoriesServiceServer(mux, pluginsServer, gwArgs); err != nil {
		return err
	}

	// The gRPC Health checker reports on all connected services.
	checker := grpchealth.NewStaticChecker(
		pluginsConnect.PluginsServiceName,
	)
	mux.Handle(grpchealth.NewHandler(checker))

	// Finally, link the new mux so that all other requests are handled by the gateway
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gwArgs.Mux.ServeHTTP(w, r)
	}))

	if serveOpts.UnsafeLocalDevKubeconfig {
		log.Warning("Using the local Kubeconfig file instead of the actual in-cluster's config. This is not recommended except for development purposes.")
	}

	log.Infof("Starting server on %q", listenAddr)
	if err := http.ListenAndServe(listenAddr, h2c.NewHandler(mux, &http2.Server{})); err != nil {
		log.Fatalf("Failed to server: %+v", err)
	}

	return nil
}

func registerPackagesServiceServer(mux *http.ServeMux, pluginsServer *pluginsv1alpha1.PluginsServer, gwArgs core.GatewayHandlerArgs) error {
	// Ask the plugins server for plugins with GRPC servers that fulfil the core
	// packaging v1alpha1 API, then pass to the constructor below.
	// The argument for the reflect.TypeOf is based on what grpc-go
	// does itself at:
	// https://github.com/grpc/grpc-go/blob/v1.38.0/server.go#L621
	packagingPlugins := pluginsServer.GetPluginsSatisfyingInterface(reflect.TypeOf((*packagesConnect.PackagesServiceHandler)(nil)).Elem())

	// Create the core.packages server and register it for both grpc and http.
	packagesServer, err := packagesv1alpha1.NewPackagesServer(packagingPlugins)
	if err != nil {
		return fmt.Errorf("failed to create core.packages.v1alpha1 server: %w", err)
	}

	// original version
	// mux.Handle(packagesConnect.NewPackagesServiceHandler(packagesServer))
	
	// interceptor 적용
	// OPENAPI_HOST, KUBEAPPS_SA_NAMESPACE, KUBEAPPS_SA_NAME 환경변수를 ConfigMap이나 Deployment spec에 넣어둡니다.
    // Go 코드에서 os.Getenv로 읽어 saTokenInterceptor 구조체 생성 시 환경변수 값을 전달
    // 이후 interceptor가 요청마다 getSATokenFromAPI를 호출해 최신 SA 토큰을 가져옴
    // updated at: 250923
	// updated by: 이호형
	mux.Handle(packagesConnect.NewPackagesServiceHandler(
		packagesServer,
		connect.WithInterceptors(&saTokenInterceptor{
			openApiHost: os.Getenv("OPENAPI_HOST"),
			saNamespace: os.Getenv("KUBEAPPS_SA_NAMESPACE"),
			saName:      os.Getenv("KUBEAPPS_SA_NAME"),
		}),
	))

	err = packagesGRPCv1alpha1.RegisterPackagesServiceHandlerFromEndpoint(gwArgs.Ctx, gwArgs.Mux, gwArgs.Addr, gwArgs.DialOptions)
	if err != nil {
		return fmt.Errorf("failed to register core.packages handler for gateway: %v", err)
	}
	return nil
}

func registerRepositoriesServiceServer(mux *http.ServeMux, pluginsServer *pluginsv1alpha1.PluginsServer, gwArgs core.GatewayHandlerArgs) error {
	// see comment in registerPackagesServiceServer
	repositoriesPlugins := pluginsServer.GetPluginsSatisfyingInterface(reflect.TypeOf((*packagesConnect.RepositoriesServiceHandler)(nil)).Elem())

	// Create the core.packages server and register it for both grpc and http.
	repoServer, err := packagesv1alpha1.NewRepositoriesServer(repositoriesPlugins)
	if err != nil {
		return fmt.Errorf("failed to create core.packages.v1alpha1 server: %w", err)
	}
	mux.Handle(packagesConnect.NewRepositoriesServiceHandler(repoServer))

	err = packagesGRPCv1alpha1.RegisterRepositoriesServiceHandlerFromEndpoint(gwArgs.Ctx, gwArgs.Mux, gwArgs.Addr, gwArgs.DialOptions)
	if err != nil {
		return fmt.Errorf("failed to register core.packages handler for gateway: %v", err)
	}
	return nil
}

// Create a gateway mux that does not emit unpopulated fields.
func gatewayMux() (*runtime.ServeMux, error) {
	gwmux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				EmitUnpopulated: false,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
	)

	// TODO(agamez): remove these '/openapi.json' and '/docs' paths. They are serving a
	// static 'swagger-ui' dashboard with hardcoded values just intended for development purposes.
	// This docs will eventually converge into the docs already (properly) served by the dashboard
	err := gwmux.HandlePath(http.MethodGet, "/openapi.json", runtime.HandlerFunc(func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
		http.ServeFile(w, r, "docs/kubeapps-apis.swagger.json")
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to serve: %v", err)
	}

	err = gwmux.HandlePath(http.MethodGet, "/docs", runtime.HandlerFunc(func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
		http.ServeFile(w, r, "docs/index.html")
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to serve: %v", err)
	}

	svcRestConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve in cluster configuration: %v", err)
	}
	coreClientSet, err := kubernetes.NewForConfig(svcRestConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve clientset: %v", err)
	}

	// TODO(rcastelblanq) Move this endpoint to the Operators plugin when implementing #4920
	// Proxies the operator icon request to K8s
	err = gwmux.HandlePath(http.MethodGet, "/operators/namespaces/{namespace}/operator/{name}/logo", func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
		namespace := pathParams["namespace"]
		name := pathParams["name"]

		logoBytes, err := coreClientSet.RESTClient().Get().AbsPath(fmt.Sprintf("/apis/packages.operators.coreos.com/v1/namespaces/%s/packagemanifests/%s/icon", namespace, name)).Do(context.TODO()).Raw()
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to retrieve operator logo: %v", err), http.StatusInternalServerError)
			return
		}

		contentType := http.DetectContentType(logoBytes)
		if strings.Contains(contentType, "text/") {
			// DetectContentType is unable to return svg icons since they are in fact text
			contentType = "image/svg+xml"
		}
		w.Header().Set("Content-Type", contentType)
		_, err = w.Write(logoBytes)
		if err != nil {
			return
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serve: %v", err)
	}

	return gwmux, nil
}

// Registers the pluginsServer with the mux and gateway.
func registerPluginsServiceServer(mux *http.ServeMux, pluginsServer *pluginsv1alpha1.PluginsServer, gwArgs core.GatewayHandlerArgs) error {
	mux.Handle(pluginsConnect.NewPluginsServiceHandler(pluginsServer))
	err := pluginsGRPCv1alpha1.RegisterPluginsServiceHandlerFromEndpoint(gwArgs.Ctx, gwArgs.Mux, gwArgs.Addr, gwArgs.DialOptions)
	if err != nil {
		return fmt.Errorf("failed to register core.plugins handler for gateway: %v", err)
	}
	return nil
}
