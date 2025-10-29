package commands

import (
	"encoding/json"
	"fmt"
	"os"
)

// Estructura para manejar la sesión activa
type Session struct {
	User        string
	Password    string // Agregado para mantener consistencia
	Group       string
	PartitionID string
	UID         int64 // Agregado
	GID         int64 // Agregado
	IsActive    bool
	IsRoot      bool // Nuevo campo para identificar si es root
}

// Variable global para la sesión activa
var currentSession *Session

// Cuando es true, los checks de sesión se omiten (útil para ejecutar scripts desde el endpoint HTTP)
var AllowCommandsWithoutSession bool = false

// Setter para controlar el comportamiento desde fuera del paquete
func SetAllowCommandsWithoutSession(v bool) {
	AllowCommandsWithoutSession = v
}

// Iniciar sesión
func StartSession(user, group, partitionID string, uid, gid int64, isRoot bool) {
	currentSession = &Session{
		User:        user,
		Group:       group,
		PartitionID: partitionID,
		UID:         uid,
		GID:         gid,
		IsActive:    true,
		IsRoot:      isRoot,
	}
	// Persistir sesión a disco para que otras instancias/procesos la lean
	saveSessionToFile()
}

// Cerrar sesión
func EndSession() {
	currentSession = nil
	// Eliminar archivo de sesión
	_ = os.Remove(sessionFilePath())
}

// Verificar si hay sesión activa
func IsSessionActive() bool {
	return currentSession != nil && currentSession.IsActive
}

// Obtener sesión actual
func GetCurrentSession() *Session {
	if currentSession != nil && currentSession.IsActive {
		return currentSession
	}

	// Intentar cargar sesión desde archivo si no está en memoria
	loadSessionFromFile()
	if currentSession != nil && currentSession.IsActive {
		return currentSession
	}

	// Si se permite ejecutar comandos sin sesión (modo script desde GUI),
	// devolver una sesión temporal que tenga privilegios root para permitir
	// que los comandos del script se ejecuten sin bloqueo.
	if AllowCommandsWithoutSession {
		// Intentar determinar una partición por defecto (la primera montada)
		defaultPartition := ""
		if parts := GetMountedPartitions(); len(parts) > 0 {
			defaultPartition = parts[0].ID
		}

		tmp := &Session{
			User:        "__auto__",
			Group:       "",
			PartitionID: defaultPartition,
			UID:         0,
			GID:         0,
			IsActive:    true,
			IsRoot:      true,
		}
		currentSession = tmp
		return currentSession
	}

	return nil
}

// Establecer sesión actual (para compatibilidad con login.go)
func SetCurrentSession(session *Session) {
	currentSession = session
}

// Limpiar sesión actual
func ClearCurrentSession() {
	currentSession = nil
}

// Verificar si el usuario actual es root
func IsRootUser() bool {
	if currentSession == nil || !currentSession.IsActive {
		return false
	}
	return currentSession.IsRoot
}

// Obtener el ID de partición del usuario actual
func GetUserPartitionID() string {
	if currentSession == nil || !currentSession.IsActive {
		return ""
	}
	return currentSession.PartitionID
}

// Validar si el usuario tiene acceso a una partición específica
func ValidatePartitionAccess(partitionID string) error {
	if currentSession == nil || !currentSession.IsActive {
		return fmt.Errorf("no hay sesión activa")
	}

	// Root tiene acceso a todas las particiones
	if currentSession.IsRoot {
		return nil
	}

	// Usuarios normales solo pueden acceder a su partición asignada
	if currentSession.PartitionID != partitionID {
		return fmt.Errorf("acceso denegado: solo puede acceder a la partición '%s'", currentSession.PartitionID)
	}

	return nil
}

// Validar acceso a un disco específico
func ValidateDiskAccess(diskPath string) error {
	if currentSession == nil || !currentSession.IsActive {
		return fmt.Errorf("no hay sesión activa")
	}

	// Root tiene acceso a todos los discos
	if currentSession.IsRoot {
		return nil
	}

	// Verificar si el usuario tiene una partición montada en este disco
	mounted := GetMountedPartition(currentSession.PartitionID)
	if mounted == nil {
		return fmt.Errorf("no tiene particiones montadas")
	}

	if mounted.Path != diskPath {
		return fmt.Errorf("acceso denegado: no tiene permisos para este disco")
	}

	return nil
}

// Verificar sesión y mostrar error si no existe
func RequireActiveSession() bool {
	if AllowCommandsWithoutSession {
		return true
	}

	if !IsSessionActive() {
		fmt.Println("Error: No existe una sesión activa. Use el comando 'login' para iniciar sesión.")
		return false
	}
	return true
}

// Verificar si se requieren permisos de root
func RequireRootPermission() bool {
	if AllowCommandsWithoutSession {
		return true
	}

	if !IsSessionActive() {
		fmt.Println("Error: No existe una sesión activa. Use el comando 'login' para iniciar sesión.")
		return false
	}

	if !IsRootUser() {
		fmt.Println("Error: Este comando requiere permisos de administrador (root).")
		return false
	}

	return true
}

// Mostrar información de la sesión actual
func ShowCurrentSession() {
	if session := GetCurrentSession(); session != nil {
		fmt.Printf("Sesión activa:\n")
		fmt.Printf("   👤 Usuario: %s\n", session.User)
		fmt.Printf("   👥 Grupo: %s\n", session.Group)
		fmt.Printf("   🆔 UID: %d | GID: %d\n", session.UID, session.GID)
		fmt.Printf("   📀 Partición: %s\n", session.PartitionID)
		if session.IsRoot {
			fmt.Printf("   🔑 Rol: Administrador (root)\n")
		} else {
			fmt.Printf("   🔑 Rol: Usuario estándar\n")
		}
	} else {
		fmt.Println("No hay sesión activa.")
	}
}

// Obtener información de sesión para API
func GetSessionInfo() map[string]interface{} {
	if currentSession == nil || !currentSession.IsActive {
		// Intentar cargar sesión desde archivo si existe
		loadSessionFromFile()
		if currentSession == nil || !currentSession.IsActive {
			return map[string]interface{}{
				"isLoggedIn": false,
			}
		}
	}

	return map[string]interface{}{
		"isLoggedIn":  true,
		"username":    currentSession.User,
		"group":       currentSession.Group,
		"partitionId": currentSession.PartitionID,
		"uid":         currentSession.UID,
		"gid":         currentSession.GID,
		"isRoot":      currentSession.IsRoot,
	}
}

// --- Persistencia de sesión ---
func sessionFilePath() string {
	return "/tmp/extreamfs_session.json"
}

func saveSessionToFile() {
	if currentSession == nil {
		return
	}

	// Preparar estructura exportable
	data := map[string]interface{}{
		"user":        currentSession.User,
		"group":       currentSession.Group,
		"partitionId": currentSession.PartitionID,
		"uid":         currentSession.UID,
		"gid":         currentSession.GID,
		"isActive":    currentSession.IsActive,
		"isRoot":      currentSession.IsRoot,
	}

	b, err := json.Marshal(data)
	if err != nil {
		return
	}

	_ = os.WriteFile(sessionFilePath(), b, 0644)
}

func loadSessionFromFile() {
	path := sessionFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal(b, &data); err != nil {
		return
	}

	// Reconstruir currentSession
	user, _ := data["user"].(string)
	group, _ := data["group"].(string)
	partitionId, _ := data["partitionId"].(string)
	uid := int64(0)
	gid := int64(0)
	if v, ok := data["uid"].(float64); ok {
		uid = int64(v)
	}
	if v, ok := data["gid"].(float64); ok {
		gid = int64(v)
	}
	isActive := false
	if v, ok := data["isActive"].(bool); ok {
		isActive = v
	}
	isRoot := false
	if v, ok := data["isRoot"].(bool); ok {
		isRoot = v
	}

	if isActive {
		currentSession = &Session{
			User:        user,
			Group:       group,
			PartitionID: partitionId,
			UID:         uid,
			GID:         gid,
			IsActive:    isActive,
			IsRoot:      isRoot,
		}
	}
}
