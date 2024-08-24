/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1beta1 "github.com/example/k8s-operator-dj-appserver/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// DJAppServerReconciler 负责对 DJAppServer 对象进行调解
type DJAppServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apps.dj-appserver.com,resources=djappservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.dj-appserver.com,resources=djappservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.dj-appserver.com,resources=djappservers/finalizers,verbs=update

// CreateOrUpdate 封装了创建或更新资源的逻辑
func (r *DJAppServerReconciler) CreateOrUpdate(ctx context.Context, obj client.Object, owner client.Object) error {
	logger := log.FromContext(ctx)
	// 获取资源的键（名字和命名空间）
	key := client.ObjectKeyFromObject(obj)
	// 尝试获取现有资源
	existing := obj.DeepCopyObject().(client.Object)
	err := r.Get(ctx, key, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// 资源未找到，创建新资源
			logger.Info("创建资源", "name", obj.GetName(), "kind", obj.GetObjectKind().GroupVersionKind().Kind)
			if err := r.Create(ctx, obj); err != nil {
				logger.Error(err, "创建资源失败", "name", obj.GetName())
				return err
			}
			logger.Info("资源创建成功", "name", obj.GetName())
			return nil
		}
		// 其他获取错误
		logger.Error(err, "获取资源失败", "name", obj.GetName())
		return err
	}
	// 资源已存在，更新资源
	logger.Info("更新资源", "name", obj.GetName(), "kind", obj.GetObjectKind().GroupVersionKind().Kind)
	if err := r.Update(ctx, obj); err != nil {
		logger.Error(err, "更新资源失败", "name", obj.GetName())
		return err
	}
	logger.Info("资源更新成功", "name", obj.GetName())
	return nil
}

// Reconcile 是主 Kubernetes 调解循环的一部分，旨在使集群的当前状态更接近所需状态。
// 该函数根据 DJAppServer 对象指定的状态，与实际集群状态进行比较，并执行操作使集群状态反映用户指定的状态。
func (r *DJAppServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// 创建日志记录器
	logger := log.FromContext(ctx)
	logger.Info("开始调解 DJAppServer", "name", req.NamespacedName)

	// 获取当前请求的 DJAppServer 实例
	appServer := &appsv1beta1.DJAppServer{}
	err := r.Get(ctx, req.NamespacedName, appServer)
	if err != nil {
		// 如果资源未找到，可能是因为它已被删除，此时无需重新排队
		if errors.IsNotFound(err) {
			logger.Info("未找到 DJAppServer 资源，忽略该请求")
			return ctrl.Result{}, nil
		}
		// 如果获取资源时出现错误，则重新排队请求
		logger.Error(err, "获取 DJAppServer 资源失败")
		return ctrl.Result{}, err
	}

	// 定义目标 Deployment，基于 DJAppServer CRD 规范创建 Deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appServer.Name,      // Deployment 的名称与 DJAppServer 的名称一致
			Namespace: appServer.Namespace, // Deployment 的命名空间与 DJAppServer 的命名空间一致
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: appServer.Spec.Replicas, // 设置 Deployment 的副本数
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": appServer.Name}, // 用于选择对应的 Pod
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": appServer.Name}, // 设置 Pod 的标签
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "appserver",          // 容器名称
						Image: appServer.Spec.Image, // 从 DJAppServer 规范中获取镜像地址
						Ports: []corev1.ContainerPort{{
							ContainerPort: appServer.Spec.ContainerPort, // 设置容器端口
						}},
					}},
				},
			},
		},
	}

	// 定义目标 Service，基于 DJAppServer CRD 规范创建 Service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appServer.Name,      // Service 的名称与 DJAppServer 的名称一致
			Namespace: appServer.Namespace, // Service 的命名空间与 DJAppServer 的命名空间一致
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": appServer.Name}, // 选择与 Deployment 匹配的 Pod
			Ports: []corev1.ServicePort{{
				Port:       appServer.Spec.ServicePort,                        // 设置服务端口
				TargetPort: intstr.FromInt(int(appServer.Spec.ContainerPort)), // 目标容器端口
			}},
		},
	}

	// 为 Deployment 和 Service 设置所有者引用，确保它们在 DJAppServer 被删除时一同删除
	if err := ctrl.SetControllerReference(appServer, deployment, r.Scheme); err != nil {
		logger.Error(err, "为 Deployment 设置控制器引用失败")
		return ctrl.Result{}, err
	}
	if err := ctrl.SetControllerReference(appServer, service, r.Scheme); err != nil {
		logger.Error(err, "为 Service 设置控制器引用失败")
		return ctrl.Result{}, err
	}

	// 创建或更新 Deployment
	err = r.CreateOrUpdate(ctx, deployment, appServer)
	if err != nil {
		logger.Error(err, "创建或更新 Deployment 失败")
		return ctrl.Result{}, err
	}
	logger.Info("Deployment 创建或更新成功", "name", deployment.Name)

	// 创建或更新 Service
	err = r.CreateOrUpdate(ctx, service, appServer)
	if err != nil {
		logger.Error(err, "创建或更新 Service 失败")
		return ctrl.Result{}, err
	}
	logger.Info("Service 创建或更新成功", "name", service.Name)

	// 返回成功结果，控制器的主要逻辑到此完成
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DJAppServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1beta1.DJAppServer{}). // 指定该控制器管理的对象类型为 DJAppServer
		Complete(r)
}
