package errorlib

import "encoding/json"
import "fmt"
import "reflect"
import "strings"
import "github.com/apperror/apperror/apperror/apperror"

type GeneralError interface {
    apperror.UniformError
    LoadCode() any
    LoadReason() any
    WithCode(any) GeneralError
    WithReason(any) GeneralError
}

type ProjectError interface {
    GeneralError
}

type RuntimeError interface {
    ProjectError
}

type ServiceError interface {
    RuntimeError
}

type UniformError interface {
    ServiceError
    LoadEquationFunction() func(error, error) bool
    WithEquationFunction(equation func(error, error) bool) UniformError
}

type GeneralException interface {
    UniformError
}

type ProjectException interface {
    GeneralException
}

type RuntimeException interface {
    ProjectException
}

type ServiceException interface {
    RuntimeException
}

type UniformException interface {
    ServiceException
}

// Join returns an error that wraps the given errors.
// Any nil error values are discarded.
// Join returns nil if every value in errs is nil.
func Join(errs ...error) UniformException {
    if errs == nil || len(errs) == 0 {
        return nil
    }
    n := 0
    for _, err := range errs {
        if err != nil {
            n++
        }
    }
    if n == 0 {
        return nil
    }
    validErrors := make([]error, 0, n)
    for _, err := range errs {
        if err != nil {
            validErrors = append(validErrors, err)
        }
    }
    result := NewUniformErrorBuilder().Errors(validErrors).MakeUniformException()
    // 防御性检查：防止返回含 typed-nil 指针的 UniformException 接口值，
    // 此类接口值 != nil 但持有的具体值为 nil，会导致调用方 nil 检查失效。
    if result == nil {
        return nil
    }
    if ue, ok := result.(*uniformException); ok && ue == nil {
        return nil
    }
    return result
}

// Is reports whether any error in err's tree matches target.
//
// The tree consists of err itself, followed by the errors obtained by repeatedly
// calling its Errors(), LoadErrors(), Unwrap() []error, or Unwrap() error method.
// When err wraps multiple errors, Is examines err followed by a depth-first
// traversal of its children.
//
// An error is considered to match a target if it is equal to that target or if
// it implements a method Is(error) bool such that Is(target) returns true.
//
// An error type might provide an Is method so it can be treated as equivalent
// to an existing error. For example, if MyError defines
//
//	func (m MyError) Is(target error) bool { return target == fs.ErrExist }
//
// then Is(MyError{}, fs.ErrExist) returns true. An Is method should only
// shallowly compare err and the target and not call Unwrap on either.
func Is(err, target error) bool {
    return is(err, target, make(map[any]bool))
}

// is: Is 内部递归实现，通过 visitedSet 防止循环引用导致的无限递归。
func is(err, target error, visited map[any]bool) bool {
    if err == nil || target == nil {
        return err == target
    }
    // 循环检测：对引用类型（指针/map/slice）使用底层指针地址跟踪，
    // 对可比较值类型使用值自身跟踪；非可比较值类型无法形成循环，跳过。
    {
        rv := reflect.ValueOf(err)
        var key any
        if k := rv.Kind(); k == reflect.Pointer || k == reflect.Map || k == reflect.Slice {
            if !rv.IsNil() {
                key = rv.Pointer()
            }
        }
        if key == nil && rv.Type().Comparable() {
            key = err
        }
        if key != nil {
            if visited[key] {
                return false
            }
            visited[key] = true
        }
    }
    if reflect.TypeOf(err).Comparable() && reflect.TypeOf(target).Comparable() && safeDeepEqual(err, target) {
        return true
    }
    if x, ok := err.(interface{ Is(error) bool }); ok && x.Is(target) {
        return true
    }
    if x, ok := err.(interface{ Errors() []error }); ok {
        for _, er := range x.Errors() {
            if er != nil && is(er, target, visited) {
                return true
            }
        }
    }
    if x, ok := err.(interface{ LoadErrors() []error }); ok {
        for _, er := range x.LoadErrors() {
            if er != nil && is(er, target, visited) {
                return true
            }
        }
    }
    if x, ok := err.(interface{ Unwrap() []error }); ok {
        for _, er := range x.Unwrap() {
            if er != nil && is(er, target, visited) {
                return true
            }
        }
    }
    if x, ok := err.(interface{ Unwrap() error }); ok {
        if er := x.Unwrap(); er != nil {
            return is(er, target, visited)
        }
    }
    return false
}

// As finds the first error in err's tree that matches target, and if one is found, sets
// target to that error value and returns true. Otherwise, it returns false.
//
// The tree consists of err itself, followed by the errors obtained by repeatedly
// calling its Errors(), LoadErrors(), Unwrap() []error, or Unwrap() error method.
// When err wraps multiple errors, As examines err followed by a depth-first traversal
// of its children.
//
// An error matches target if the error's concrete value is assignable to the value
// pointed to by target, or if the error has a method As(any) bool such that
// As(target) returns true. In the latter case, the As method is responsible for
// setting target.
func As(err error, target any) bool {
    if err == nil || target == nil {
        return false
    }
    val := reflect.ValueOf(target)
    typ := val.Type()
    if typ.Kind() != reflect.Pointer || val.IsNil() {
        return false
    }
    targetType := typ.Elem()
    if targetType.Kind() != reflect.Interface &&
        !targetType.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
        return false
    }
    return as(err, target, val, targetType, make(map[any]bool))
}

// as: As 内部递归实现，通过 visited 防止循环引用导致的无限递归。
func as(err error, target any, targetVal reflect.Value, targetType reflect.Type, visited map[any]bool) bool {
    if reflect.TypeOf(err).AssignableTo(targetType) {
        targetVal.Elem().Set(reflect.ValueOf(err))
        return true
    }
    // 循环检测：对引用类型（指针/map/slice）使用底层指针地址跟踪，
    // 对可比较值类型使用值自身跟踪；非可比较值类型无法形成循环，跳过。
    {
        rv := reflect.ValueOf(err)
        var key any
        if k := rv.Kind(); k == reflect.Pointer || k == reflect.Map || k == reflect.Slice {
            if !rv.IsNil() {
                key = rv.Pointer()
            }
        }
        if key == nil && rv.Type().Comparable() {
            key = err
        }
        if key != nil {
            if visited[key] {
                return false
            }
            visited[key] = true
        }
    }
    if x, ok := err.(interface{ As(any) bool }); ok && x.As(target) {
        return true
    }
    if x, ok := err.(interface{ Errors() []error }); ok {
        for _, child := range x.Errors() {
            if child != nil && as(child, target, targetVal, targetType, visited) {
                return true
            }
        }
    }
    if x, ok := err.(interface{ LoadErrors() []error }); ok {
        for _, child := range x.LoadErrors() {
            if child != nil && as(child, target, targetVal, targetType, visited) {
                return true
            }
        }
    }
    if x, ok := err.(interface{ Unwrap() []error }); ok {
        for _, child := range x.Unwrap() {
            if child != nil && as(child, target, targetVal, targetType, visited) {
                return true
            }
        }
    }
    if x, ok := err.(interface{ Unwrap() error }); ok {
        child := x.Unwrap()
        if child != nil {
            return as(child, target, targetVal, targetType, visited)
        }
    }
    return false
}

// copyAny 尝试对 any 类型执行受控深拷贝。
// 优先检查 Clone() any 接口，其次 Copy() any 接口。
// 对于未实现以上接口的 slice/map，执行逐元素浅层拷贝防止外部篡改。
// 对于 struct 类型递归深拷贝其导出字段中的指针、slice、map 等引用类型。
// 对于指针类型使用 visited map 防止循环引用。
// 对于基本类型直接返回原值。
func copyAny(v any) any {
    return copyAnyInternal(v, make(map[any]any))
}

// copyAnyInternal 是 copyAny 的内部递归实现，通过 visited 映射防止指针循环引用。
func copyAnyInternal(v any, visited map[any]any) any {
    if v == nil {
        return nil
    }
    if cloner, ok := v.(interface{ Clone() any }); ok {
        return cloner.Clone()
    }
    if copier, ok := v.(interface{ Copy() any }); ok {
        return copier.Copy()
    }

    rv := reflect.ValueOf(v)
    if rv.Kind() == reflect.Slice {
        if rv.IsNil() {
            return v
        }
        dst := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
        elemType := rv.Type().Elem()
        for i := 0; i < rv.Len(); i++ {
            elem := copyAnyInternal(rv.Index(i).Interface(), visited)
            if elem == nil {
                dst.Index(i).Set(reflect.Zero(elemType))
            } else {
                dst.Index(i).Set(reflect.ValueOf(elem))
            }
        }
        return dst.Interface()
    }
    if rv.Kind() == reflect.Array {
        dst := reflect.New(rv.Type()).Elem()
        elemType := rv.Type().Elem()
        for i := 0; i < rv.Len(); i++ {
            elem := copyAnyInternal(rv.Index(i).Interface(), visited)
            if elem == nil {
                dst.Index(i).Set(reflect.Zero(elemType))
            } else {
                dst.Index(i).Set(reflect.ValueOf(elem))
            }
        }
        return dst.Interface()
    }
    if rv.Kind() == reflect.Map {
        if rv.IsNil() {
            return v
        }
        keyType := rv.Type().Key()
        valType := rv.Type().Elem()
        dst := reflect.MakeMap(rv.Type())
        for _, key := range rv.MapKeys() {
            newKey := copyAnyInternal(key.Interface(), visited)
            if newKey == nil {
                newKey = reflect.Zero(keyType).Interface()
            }
            newVal := copyAnyInternal(rv.MapIndex(key).Interface(), visited)
            if newVal == nil {
                newVal = reflect.Zero(valType).Interface()
            }
            dst.SetMapIndex(reflect.ValueOf(newKey), reflect.ValueOf(newVal))
        }
        return dst.Interface()
    }
    if rv.Kind() == reflect.Pointer {
        if rv.IsNil() {
            return nil
        }
        // 使用 v 自身作为 visited key：指针值始终可比较，且持有原对象引用防止 GC 回收。
        if existing, ok := visited[v]; ok {
            return existing
        }
        // 预注册占位副本，确保后续遇到同一指针时直接复用
        placeholder := reflect.New(rv.Type().Elem())
        visited[v] = placeholder.Interface()
        elem := copyAnyInternal(rv.Elem().Interface(), visited)
        if elem != nil {
            placeholder.Elem().Set(reflect.ValueOf(elem))
        }
        return placeholder.Interface()
    }
    if rv.Kind() == reflect.Struct {
        dst := reflect.New(rv.Type()).Elem()
        for i := 0; i < rv.NumField(); i++ {
            ft := rv.Type().Field(i)
            fv := rv.Field(i)
            if !ft.IsExported() {
                kind := ft.Type.Kind()
                if kind == reflect.Interface || kind == reflect.Pointer || kind == reflect.Slice || kind == reflect.Map {
                    if !fv.IsNil() && fv.CanInterface() {
                        if copied := copyAnyInternal(fv.Interface(), visited); copied != nil {
                            dst.Field(i).Set(reflect.ValueOf(copied))
                            continue
                        }
                    }
                    if fv.IsNil() {
                        dst.Field(i).Set(reflect.Zero(ft.Type))
                        continue
                    }
                }
                dst.Field(i).Set(fv)
                continue
            }
            switch fv.Kind() {
            case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Array:
                copied := copyAnyInternal(fv.Interface(), visited)
                if copied != nil {
                    dst.Field(i).Set(reflect.ValueOf(copied))
                }
            default:
                dst.Field(i).Set(fv)
            }
        }
        return dst.Interface()
    }
    return v
}

// safeDeepEqual 安全地执行深比较，通过 recover 防护 reflect.DeepEqual 对含函数/通道等
// 不可比较类型的 panic 风险。
func safeDeepEqual(a, b any) (ok bool) {
    defer func() {
        if r := recover(); r != nil {
            ok = false
        }
    }()
    return reflect.DeepEqual(a, b)
}

// errorsEquals 使用 safeDeepEqual 逐个比较两个 error 切片，
// 基于值相等语义，避免 Is 的"错误匹配"语义造成误判。
func errorsEquals(a, b []error) (ok bool) {
    defer func() {
        if r := recover(); r != nil {
            ok = false
        }
    }()
    if len(a) != len(b) {
        return false
    }
    for i := range a {
        if a[i] == nil && b[i] == nil {
            continue
        }
        if a[i] == nil || b[i] == nil || !safeDeepEqual(a[i], b[i]) {
            return false
        }
    }
    return true
}

const (
    statusSerializable    = 1  // 明确可序列化
    statusNotSerializable = 0  // 明确不可序列化
    statusUncertain       = -1 // 存在类型循环，无法静态确定
)

// jsonSerializable 递归检查 reflect.Type 是否能被 encoding/json 安全序列化。
// 函数、通道、复数等类型无法被 json.Marshal 处理，会被返回 UnsupportedTypeError。
// 如果类型实现了 json.Marshaler 接口，则认为可安全序列化。
// 对于存在自引用循环的类型，视为不可序列化（返回 false）—— 通过包装确保安全。
func jsonSerializable(t reflect.Type) bool {
    return jsonSerializableInternal(t, make(map[reflect.Type]bool)) == statusSerializable
}

// jsonSerializableInternal 是 jsonSerializable 的内部递归实现，
// 通过 visiting 映射检测循环引用的类型，防止栈溢出。
// 返回三态：可序列化 / 不可序列化 / 不确定（类型循环时）。
func jsonSerializableInternal(t reflect.Type, visited map[reflect.Type]bool) int {
    // 实现 json.Marshaler 接口的可自行控制序列化，视为安全
    marshalerType := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
    if t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType) {
        return statusSerializable
    }
    switch t.Kind() {
    case reflect.Func, reflect.Chan, reflect.Complex64, reflect.Complex128:
        return statusNotSerializable
    case reflect.Pointer, reflect.Slice, reflect.Array:
        if visited[t] {
            return statusUncertain
        }
        visited[t] = true
        result := jsonSerializableInternal(t.Elem(), visited)
        delete(visited, t)
        return result
    case reflect.Map:
        if visited[t] {
            return statusUncertain
        }
        visited[t] = true
        result := jsonSerializableInternal(t.Key(), visited)
        if result == statusNotSerializable {
            delete(visited, t)
            return statusNotSerializable
        }
        result = jsonSerializableInternal(t.Elem(), visited)
        delete(visited, t)
        return result
    case reflect.Struct:
        if visited[t] {
            // 结构体自引用循环：所有导出字段已在首次遍历时检查完毕，
            // 此处返回 uncertain 表示无法判定循环路径上的类型是否可序列化。
            return statusUncertain
        }
        visited[t] = true
        for i := 0; i < t.NumField(); i++ {
            f := t.Field(i)
            if !f.IsExported() {
                continue
            }
            if jsonSerializableInternal(f.Type, visited) == statusNotSerializable {
                delete(visited, t)
                return statusNotSerializable
            }
        }
        delete(visited, t)
        return statusSerializable
    default:
        // 基本类型（Bool, Int*, Uint*, Float*, String, Interface 等）均可安全序列化
        return statusSerializable
    }
}

// needsWrap 判断 err 是否需要包装为 UniformException。
// 当 err 的类型没有导出字段时，json.Marshal 无法序列化有效信息，需要包装。
func needsWrap(err error) bool {
    if err == nil {
        return false
    }
    // 如果已实现 json.Marshaler 接口（包括值接收者和指针接收者），
    // 可自行控制序列化结果，无需包装。
    if _, ok := err.(json.Marshaler); ok {
        return false
    }
    marshalerType := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
    t := reflect.TypeOf(err)
    if t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType) {
        return false
    }
    for t.Kind() == reflect.Pointer {
        t = t.Elem()
    }
    switch t.Kind() {
    case reflect.Struct:
        hasExported := false
        for i := 0; i < t.NumField(); i++ {
            if t.Field(i).IsExported() {
                hasExported = true
                if !jsonSerializable(t.Field(i).Type) {
                    return true
                }
            }
        }
        if !hasExported {
            return true
        }
        // 静态检查通过，运行时验证：any/interface 类型字段在运行时可能包含不可序列化的值
        if _, marshalErr := json.Marshal(err); marshalErr != nil {
            return true
        }
        return false
    case reflect.Map, reflect.Slice, reflect.Array:
        // 对容器类型（包括命名类型），进一步检查元素类型是否可序列化
        if !jsonSerializable(t) {
            return true
        }
        // 静态检查通过，运行时验证
        if _, marshalErr := json.Marshal(err); marshalErr != nil {
            return true
        }
        return false
    case reflect.String, reflect.Bool,
        reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
        reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
        reflect.Float32, reflect.Float64:
        return false
    default:
        return true
    }
}

// normalizeErrors 深拷贝 src 切片并过滤 nil，再将其中需要包装的 error 转换为 jsonSafeError，
// 保证 JSON 序列化能正确输出错误信息，且返回的切片不与 src 共享底层数组。
func normalizeErrors(src []error) []error {
    if src == nil {
        return nil
    }
    dst := make([]error, 0, len(src))
    for _, err := range src {
        if err == nil {
            continue
        }
        // 已经是 jsonSafeError 的不再重复包装（幂等保护）
        if _, ok := err.(*jsonSafeError); ok {
            dst = append(dst, err)
            continue
        }
        if needsWrap(err) {
            dst = append(
                dst, &jsonSafeError{
                    msg: err.Error(),
                    err: err, // 保留原始错误引用，供 Is/As 委派
                },
            )
            continue
        }
        // 深拷贝避免与外部共享底层引用（如 map、slice 等可变字段）
        if copied, ok := copyAny(err).(error); ok && copied != nil {
            dst = append(dst, copied)
        } else {
            dst = append(dst, err)
        }
    }
    if len(dst) == 0 {
        return nil
    }
    return dst
}

// jsonSafeError 包装没有导出字段的 error，保留原始错误引用用于 Is/As 匹配，
// 同时提供安全的 JSON 序列化（输出为字符串而非 {}）。
type jsonSafeError struct {
    msg string // 错误消息，用于 Error() 和 JSON 序列化
    err error  // 原始错误，用于 Is/As 委派
}

func (e *jsonSafeError) Error() string { return e.msg }

func (e *jsonSafeError) Unwrap() error { return e.err }

// Is 委派给原始 error，使用本包的 Is 确保一致的循环检测和自定义 Is 语义
func (e *jsonSafeError) Is(target error) bool {
    return Is(e.err, target)
}

// As 委派给原始 error，确保 errors.As(result, &targetType) 能匹配
func (e *jsonSafeError) As(target any) bool {
    return As(e.err, target)
}

// MarshalJSON 序列化为消息字符串，避免 {} 空对象
func (e *jsonSafeError) MarshalJSON() ([]byte, error) {
    return json.Marshal(e.msg)
}

// uniformException 统一异常
type uniformException struct {
    Code             any                       `json:"code,omitempty"`        // Code 编码
    Data             any                       `json:"data,omitempty"`        // Data 数据
    Description      apperror.ErrorDescription `json:"description,omitempty"` // Description 描述
    Detail           any                       `json:"detail,omitempty"`      // Detail 详情
    Errors           []error                   `json:"errors,omitempty"`      // Errors 错误集合
    Information      any                       `json:"information,omitempty"` // Information 信息
    Message          string                    `json:"message,omitempty"`     // Message 消息
    Profile          any                       `json:"profile,omitempty"`     // Profile
    Reason           any                       `json:"reason,omitempty"`      // Reason 原因
    Status           any                       `json:"status,omitempty"`      // Status 状态
    equationFunction func(error, error) bool
}

// clone 深拷贝 uniformException，确保 Errors 切片不与原对象共享底层数组，
// 并对 Data、Detail、Information、Profile、Status 执行受控深拷贝。
func (e *uniformException) clone() *uniformException {
    if e == nil {
        return nil
    }
    ex := new(uniformException)
    *ex = *e
    ex.Code = copyAny(e.Code)
    ex.Data = copyAny(e.Data)
    // 深拷贝 Description 内部的可变引用类型字段（Data、Detail、Information、Status、Type）
    ex.Description.Data = copyAny(e.Description.Data)
    ex.Description.Detail = copyAny(e.Description.Detail)
    ex.Description.Information = copyAny(e.Description.Information)
    ex.Description.Status = copyAny(e.Description.Status)
    ex.Description.Type = copyAny(e.Description.Type)
    {
        // 使用反射深度复制 ErrorDescription 中所有 any 类型的导出字段。
        // 相比手动列出每个字段，这种方式在 ErrorDescription 结构体新增字段时能自动覆盖，
        // 降低维护风险。
        srcVal := reflect.ValueOf(&e.Description).Elem()
        dstVal := reflect.ValueOf(&ex.Description).Elem()
        for i := 0; i < srcVal.NumField(); i++ {
            ft := srcVal.Type().Field(i)
            if !ft.IsExported() {
                continue
            }
            if ft.Type.Kind() == reflect.Interface {
                srcField := srcVal.Field(i)
                if !srcField.IsNil() {
                    copied := copyAny(srcField.Interface())
                    dstVal.Field(i).Set(reflect.ValueOf(copied))
                }
            }
        }
    }
    ex.Errors = normalizeErrors(e.LoadErrors())
    ex.Detail = copyAny(e.Detail)
    ex.Information = copyAny(e.Information)
    ex.Profile = copyAny(e.Profile)
    ex.Reason = copyAny(e.Reason)
    ex.Status = copyAny(e.Status)
    // equationFunction 在 *ex = *e 时已被浅拷贝；此处显式赋值以明确表明
    // 函数值在 clone 间是独立持有的，不依赖 struct 值拷贝的隐式行为。
    ex.equationFunction = e.equationFunction
    return ex
}

func (e *uniformException) Error() string {
    if e == nil {
        return ""
    }
    // 使用 clone() 深拷贝并规范化 Errors，防止 JSON 序列化时隐式循环
    if bytes, err := json.Marshal(e.clone()); err == nil {
        return string(bytes)
    }
    //  json.Marshal 失败时，逐字段尝试序列化以产生合法 JSON
    jsonVal := func(v any) string {
        if v == nil {
            return "null"
        }
        if b, err := json.Marshal(v); err == nil {
            return string(b)
        }
        return fmt.Sprintf("%q", fmt.Sprintf("%v", v))
    }
    jsonErrs := func(errs []error) string {
        if errs == nil {
            return "null"
        }
        var s strings.Builder
        s.WriteString("[")
        for i, er := range errs {
            if i > 0 {
                s.WriteString(",")
            }
            if er == nil {
                s.WriteString("null")
            } else {
                s.WriteString(fmt.Sprintf("%q", er.Error()))
            }
        }
        s.WriteString("]")
        return s.String()
    }
    return fmt.Sprintf(
        `{
            "Code":%s,
            "Data":%s,
            "Description":%s,
            "Detail":%s,
            "Errors":%s,
            "Information":%s,
            "Message":%s,
            "Profile":%s,
            "Reason":%s,
            "Status":%s
        }`,
        jsonVal(e.Code),
        jsonVal(e.Data),
        jsonVal(e.Description),
        jsonVal(e.Detail),
        jsonErrs(e.LoadErrors()),
        jsonVal(e.Information),
        fmt.Sprintf("%q", e.Message),
        jsonVal(e.Profile),
        jsonVal(e.Reason),
        jsonVal(e.Status),
    )
}

func (e *uniformException) String() string {
    if e == nil {
        return ""
    }
    return e.Error()
}

func (e *uniformException) Is(target error) bool {
    if e == nil {
        return e == target
    }
    if e.equationFunction != nil {
        return e.equationFunction(e, target)
    }
    var generalError GeneralError
    if As(target, &generalError) {
        for _, fn := range []func(a, b GeneralError) bool{
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadCode(), b.LoadCode()) },
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadData(), b.LoadData()) },
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadDescription(), b.LoadDescription()) },
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadDetail(), b.LoadDetail()) },
            func(a, b GeneralError) bool { return errorsEquals(a.LoadErrors(), b.LoadErrors()) },
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadInformation(), b.LoadInformation()) },
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadMessage(), b.LoadMessage()) },
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadProfile(), b.LoadProfile()) },
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadReason(), b.LoadReason()) },
            func(a, b GeneralError) bool { return safeDeepEqual(a.LoadStatus(), b.LoadStatus()) },
        } {
            if !fn(e, generalError) {
                return false
            }
        }
        return true
    }
    return e == target
}

func (e *uniformException) Unwrap() []error {
    return e.LoadErrors()
}

func (e *uniformException) LoadCode() any {
    if e == nil {
        return nil
    }
    return copyAny(e.Code)
}

func (e *uniformException) LoadData() any {
    if e == nil {
        return nil
    }
    return copyAny(e.Data)
}

func (e *uniformException) LoadDescription() apperror.ErrorDescription {
    if e == nil {
        return apperror.ErrorDescription{}
    }
    desc := e.Description
    desc.Data = copyAny(desc.Data)
    desc.Detail = copyAny(desc.Detail)
    desc.Information = copyAny(desc.Information)
    desc.Status = copyAny(desc.Status)
    desc.Type = copyAny(desc.Type)
    // 使用反射深度复制 ErrorDescription 中所有 any 类型的导出字段。
    // 相比手动列出每个字段，这种方式在 ErrorDescription 结构体新增字段时能自动覆盖，
    // 降低维护风险。
    srcVal := reflect.ValueOf(&e.Description).Elem()
    dstVal := reflect.ValueOf(&desc).Elem()
    for i := 0; i < srcVal.NumField(); i++ {
        ft := srcVal.Type().Field(i)
        if !ft.IsExported() {
            continue
        }
        if ft.Type.Kind() == reflect.Interface {
            srcField := srcVal.Field(i)
            if !srcField.IsNil() {
                copied := copyAny(srcField.Interface())
                dstVal.Field(i).Set(reflect.ValueOf(copied))
            }
        }
    }
    return desc
}

func (e *uniformException) LoadDetail() any {
    if e == nil {
        return nil
    }
    return copyAny(e.Detail)
}

func (e *uniformException) LoadErrors() []error {
    if e == nil || e.Errors == nil {
        return nil
    }
    dst := make([]error, 0, len(e.Errors))
    for _, err := range e.Errors {
        if err == nil {
            continue
        }
        // 已经是 jsonSafeError 的不再重复包装（幂等保护）
        if _, ok := err.(*jsonSafeError); ok {
            dst = append(dst, err)
            continue
        }
        if needsWrap(err) {
            dst = append(
                dst, &jsonSafeError{
                    msg: err.Error(),
                    err: err,
                },
            )
            continue
        }
        if copied, ok := copyAny(err).(error); ok && copied != nil {
            dst = append(dst, copied)
        } else {
            dst = append(dst, err)
        }
    }
    if len(dst) == 0 {
        return nil
    }
    return dst
}

func (e *uniformException) LoadInformation() any {
    if e == nil {
        return nil
    }
    return copyAny(e.Information)
}

func (e *uniformException) LoadMessage() string {
    if e == nil {
        return ""
    }
    return e.Message
}

func (e *uniformException) LoadProfile() any {
    if e == nil {
        return nil
    }
    return copyAny(e.Profile)
}

func (e *uniformException) LoadReason() any {
    if e == nil {
        return nil
    }
    return copyAny(e.Reason)
}

func (e *uniformException) LoadStatus() any {
    if e == nil {
        return nil
    }
    return copyAny(e.Status)
}

func (e *uniformException) LoadEquationFunction() func(error, error) bool {
    if e == nil {
        return nil
    }
    return e.equationFunction
}

func (e *uniformException) WithCode(code any) GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Code = copyAny(code)
    return ex
}

func (e *uniformException) WithData(data any) apperror.GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Data = copyAny(data)
    return ex
}

func (e *uniformException) WithDescription(description apperror.ErrorDescription) apperror.GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    desc := description
    desc.Data = copyAny(desc.Data)
    desc.Detail = copyAny(desc.Detail)
    desc.Information = copyAny(desc.Information)
    desc.Status = copyAny(desc.Status)
    desc.Type = copyAny(desc.Type)
    // 使用反射深度复制 ErrorDescription 中所有 any 类型的导出字段。
    // 相比手动列出每个字段，这种方式在 ErrorDescription 结构体新增字段时能自动覆盖，
    // 降低维护风险。
    srcVal := reflect.ValueOf(&description).Elem()
    dstVal := reflect.ValueOf(&desc).Elem()
    for i := 0; i < srcVal.NumField(); i++ {
        ft := srcVal.Type().Field(i)
        if !ft.IsExported() {
            continue
        }
        if ft.Type.Kind() == reflect.Interface {
            srcField := srcVal.Field(i)
            if !srcField.IsNil() {
                copied := copyAny(srcField.Interface())
                dstVal.Field(i).Set(reflect.ValueOf(copied))
            }
        }
    }
    ex.Description = desc
    return ex
}

func (e *uniformException) WithDetail(detail any) apperror.GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Detail = copyAny(detail)
    return ex
}

func (e *uniformException) WithErrors(errors []error) apperror.GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Errors = normalizeErrors(errors)
    return ex
}

func (e *uniformException) WithInformation(information any) apperror.GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Information = copyAny(information)
    return ex
}

func (e *uniformException) WithMessage(message string) apperror.GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Message = message
    return ex
}

func (e *uniformException) WithProfile(profile any) apperror.GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Profile = copyAny(profile)
    return ex
}

func (e *uniformException) WithReason(reason any) GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Reason = copyAny(reason)
    return ex
}

func (e *uniformException) WithStatus(status any) apperror.GeneralError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.Status = copyAny(status)
    return ex
}

func (e *uniformException) WithEquationFunction(equation func(error, error) bool) UniformError {
    if e == nil {
        return nil
    }
    ex := e.clone()
    ex.equationFunction = equation
    return ex
}

func NewUniformErrorBuilder() UniformErrorBasicBuilder {
    return &uniformExceptionBuilder{}
}

type UniformErrorBasicBuilder interface {
    Code(any) UniformErrorBasicBuilder
    Data(any) UniformErrorBasicBuilder
    Description(apperror.ErrorDescription) UniformErrorBasicBuilder
    Detail(any) UniformErrorBasicBuilder
    Errors([]error) UniformErrorBasicBuilder
    Information(any) UniformErrorBasicBuilder
    Message(string) UniformErrorBasicBuilder
    Profile(any) UniformErrorBasicBuilder
    Reason(any) UniformErrorBasicBuilder
    Status(any) UniformErrorBasicBuilder
    EquationFunction(func(error, error) bool) UniformErrorBasicBuilder
    CreateGeneralError() GeneralError
    CreateProjectError() ProjectError
    CreateRuntimeError() RuntimeError
    CreateServiceError() ServiceError
    CreateUniformError() UniformError
    CreateGeneralException() GeneralException
    CreateProjectException() ProjectException
    CreateRuntimeException() RuntimeException
    CreateServiceException() ServiceException
    CreateUniformException() UniformException
    MakeGeneralError() GeneralError
    MakeProjectError() ProjectError
    MakeRuntimeError() RuntimeError
    MakeServiceError() ServiceError
    MakeUniformError() UniformError
    MakeGeneralException() GeneralException
    MakeProjectException() ProjectException
    MakeRuntimeException() RuntimeException
    MakeServiceException() ServiceException
    MakeUniformException() UniformException
}

type uniformExceptionBuilder struct {
    uniformException uniformException
}

func (b *uniformExceptionBuilder) Code(code any) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Code(code)
    }
    b.uniformException.Code = copyAny(code)
    return b
}

func (b *uniformExceptionBuilder) Data(data any) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Data(data)
    }
    b.uniformException.Data = copyAny(data)
    return b
}

func (b *uniformExceptionBuilder) Description(description apperror.ErrorDescription) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Description(description)
    }
    desc := description
    desc.Data = copyAny(desc.Data)
    desc.Detail = copyAny(desc.Detail)
    desc.Information = copyAny(desc.Information)
    desc.Status = copyAny(desc.Status)
    desc.Type = copyAny(desc.Type)
    // 使用反射深度复制 ErrorDescription 中所有 any 类型的导出字段。
    // 相比手动列出每个字段，这种方式在 ErrorDescription 结构体新增字段时能自动覆盖，
    // 降低维护风险。
    srcVal := reflect.ValueOf(&description).Elem()
    dstVal := reflect.ValueOf(&desc).Elem()
    for i := 0; i < srcVal.NumField(); i++ {
        ft := srcVal.Type().Field(i)
        if !ft.IsExported() {
            continue
        }
        if ft.Type.Kind() == reflect.Interface {
            srcField := srcVal.Field(i)
            if !srcField.IsNil() {
                copied := copyAny(srcField.Interface())
                dstVal.Field(i).Set(reflect.ValueOf(copied))
            }
        }
    }
    b.uniformException.Description = desc
    return b
}

func (b *uniformExceptionBuilder) Detail(detail any) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Detail(detail)
    }
    b.uniformException.Detail = copyAny(detail)
    return b
}

func (b *uniformExceptionBuilder) Errors(errors []error) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Errors(errors)
    }
    b.uniformException.Errors = normalizeErrors(errors)
    return b
}

func (b *uniformExceptionBuilder) Information(information any) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Information(information)
    }
    b.uniformException.Information = copyAny(information)
    return b
}

func (b *uniformExceptionBuilder) Message(message string) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Message(message)
    }
    b.uniformException.Message = message
    return b
}

func (b *uniformExceptionBuilder) Profile(profile any) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Profile(profile)
    }
    b.uniformException.Profile = copyAny(profile)
    return b
}

func (b *uniformExceptionBuilder) Reason(reason any) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Reason(reason)
    }
    b.uniformException.Reason = copyAny(reason)
    return b
}

func (b *uniformExceptionBuilder) Status(status any) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().Status(status)
    }
    b.uniformException.Status = copyAny(status)
    return b
}

func (b *uniformExceptionBuilder) EquationFunction(equation func(error, error) bool) UniformErrorBasicBuilder {
    if b == nil {
        return NewUniformErrorBuilder().EquationFunction(equation)
    }
    b.uniformException.equationFunction = equation
    return b
}

func (b *uniformExceptionBuilder) CreateGeneralError() GeneralError {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateProjectError() ProjectError {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateRuntimeError() RuntimeError {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateServiceError() ServiceError {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateUniformError() UniformError {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateGeneralException() GeneralException {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateProjectException() ProjectException {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateRuntimeException() RuntimeException {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateServiceException() ServiceException {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) CreateUniformException() UniformException {
    if b == nil {
        return &uniformException{}
    }
    return b.uniformException.clone()
}

func (b *uniformExceptionBuilder) MakeGeneralError() GeneralError {
    return b.CreateGeneralError()
}

func (b *uniformExceptionBuilder) MakeProjectError() ProjectError {
    return b.CreateProjectError()
}

func (b *uniformExceptionBuilder) MakeRuntimeError() RuntimeError {
    return b.CreateRuntimeError()
}

func (b *uniformExceptionBuilder) MakeServiceError() ServiceError {
    return b.CreateServiceError()
}

func (b *uniformExceptionBuilder) MakeUniformError() UniformError {
    return b.CreateUniformError()
}

func (b *uniformExceptionBuilder) MakeGeneralException() GeneralException {
    return b.CreateGeneralException()
}

func (b *uniformExceptionBuilder) MakeProjectException() ProjectException {
    return b.CreateProjectException()
}

func (b *uniformExceptionBuilder) MakeRuntimeException() RuntimeException {
    return b.CreateRuntimeException()
}

func (b *uniformExceptionBuilder) MakeServiceException() ServiceException {
    return b.CreateServiceException()
}

func (b *uniformExceptionBuilder) MakeUniformException() UniformException {
    return b.CreateUniformException()
}
