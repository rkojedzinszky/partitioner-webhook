package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// AntiAffinityAnnotation is array of topology keys
	// If true, pods will have podAntiaffinity settings based on specified topology keys
	AntiAffinityAnnotation = "kojedz.in/podantiaffinitytopologykeys"

	// NodeSelectorAnnotation is a string
	// Takes the form: "label=match[,label=match]"
	// If set, all pods will have these nodeSelectors added
	NodeSelectorAnnotation = "kojedz.in/nodeselectors"

	// TopologySpreadConstraintAnnotation is array of strings
	// For each TopologyKey a TopologySpreadConstraint is added to the Pod Spec
	TopologySpreadConstraintAnnotation = "kojedz.in/topologyspreadconstrainttopologykeys"
)

type webhook struct {
	restconfig   *rest.Config
	deserializer runtime.Decoder
}

func newWebHook(kubeConfig string) (*webhook, error) {
	runtimeScheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(runtimeScheme)
	deserializer := codecs.UniversalDeserializer()

	var restconfig *rest.Config
	var err error

	if kubeConfig != "" {
		restconfig, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
	} else {
		restconfig, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, fmt.Errorf("Failed to initialize kubernetes client")
	}

	return &webhook{
		restconfig:   restconfig,
		deserializer: deserializer,
	}, nil
}

func (h *webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		log.Printf("Content-Type=%s, expect application/json\n", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	if len(body) == 0 {
		log.Println("Empty body received")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	var admissionResponse *v1.AdmissionResponse
	ar := v1.AdmissionReview{}
	if _, _, err := h.deserializer.Decode(body, nil, &ar); err != nil {
		log.Printf("Can't decode body: %v\n", err)
		admissionResponse = &v1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = h.mutate(&ar)
	}

	if admissionResponse != nil {
		ar.Response = admissionResponse
		if ar.Request != nil {
			ar.Response.UID = ar.Request.UID
		}
	}

	ar.Request = nil

	resp, err := json.Marshal(ar)
	if err != nil {
		log.Println("Error marshalling json:", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_, err = w.Write(resp)
	if err != nil {
		log.Println("Failed to write response:", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func (h *webhook) mutate(a *v1.AdmissionReview) *v1.AdmissionResponse {
	req := a.Request

	if req.Kind.Kind != "Pod" {
		return &v1.AdmissionResponse{
			Allowed: true,
		}
	}

	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		log.Println(err)
		return nil
	}

	clientset, err := kubernetes.NewForConfig(h.restconfig)
	if err != nil {
		log.Println(err)
		return nil
	}

	namespace, err := clientset.CoreV1().Namespaces().Get(context.Background(), req.Namespace, metav1.GetOptions{})
	if err != nil {
		log.Println(err)
		return nil
	}

	nodeSelector := pod.Spec.NodeSelector
	if nodeSelector == nil {
		nodeSelector = make(map[string]string)
	}
	var patches []patchOperation

	// Handle NodeSelectorAnnotation
	if annotation, ok := namespace.ObjectMeta.Annotations[NodeSelectorAnnotation]; ok {
		for _, selector := range strings.Split(annotation, ",") {
			parsed := strings.Split(selector, "=")
			if len(parsed) == 2 {
				nodeSelector[parsed[0]] = parsed[1]
			}
		}

		patches = append(patches, patchOperation{
			Op:    "replace",
			Path:  "/spec/nodeSelector",
			Value: nodeSelector,
		})
	}

	// Handle AntiAffinityAnnotation
	if annotation, ok := namespace.ObjectMeta.Annotations[AntiAffinityAnnotation]; ok {
		if pod.Spec.Affinity == nil {
			patches = append(patches, patchOperation{
				Op:    "add",
				Path:  "/spec/affinity",
				Value: make(map[string]interface{}),
			})
		}

		if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil {
			patches = append(patches, patchOperation{
				Op:    "add",
				Path:  "/spec/affinity/podAntiAffinity",
				Value: make(map[string]interface{}),
			})
		}

		if pod.Spec.Affinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil || pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			patches = append(patches, patchOperation{
				Op:    "add",
				Path:  "/spec/affinity/podAntiAffinity/requiredDuringSchedulingIgnoredDuringExecution",
				Value: make([]interface{}, 0),
			})
		}

		for _, topologyKey := range strings.Split(annotation, ",") {
			if len(topologyKey) == 0 {
				continue
			}

			patches = append(patches, patchOperation{
				Op:   "add",
				Path: "/spec/affinity/podAntiAffinity/requiredDuringSchedulingIgnoredDuringExecution/-",
				Value: &corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: pod.ObjectMeta.Labels,
					},
					TopologyKey: topologyKey,
				},
			})
		}
	}

	// Handle TopologySpreadConstraintAnnotation
	if annotation, ok := namespace.ObjectMeta.Annotations[TopologySpreadConstraintAnnotation]; ok {
		if pod.Spec.TopologySpreadConstraints == nil {
			patches = append(patches, patchOperation{
				Op:    "add",
				Path:  "/spec/topologySpreadConstraints",
				Value: make([]interface{}, 0),
			})
		}

		for _, topologyKey := range strings.Split(annotation, ",") {
			if len(topologyKey) == 0 {
				continue
			}

			patches = append(patches, patchOperation{
				Op:   "add",
				Path: "/spec/topologySpreadConstraints/-",
				Value: &corev1.TopologySpreadConstraint{
					MaxSkew:     1,
					TopologyKey: topologyKey,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: pod.ObjectMeta.Labels,
					},
					WhenUnsatisfiable: corev1.DoNotSchedule,
				},
			})
		}
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		log.Println("Failed to serialize patches")
		return nil
	}

	patchType := v1.PatchTypeJSONPatch
	return &v1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}
}
