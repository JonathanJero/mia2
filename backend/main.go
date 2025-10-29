package main

import (
	"backend/commands"
	"bufio"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// Estructuras para la API HTTP
type CommandRequest struct {
	Command string `json:"command"`
}

type CommandResponse struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

func main() {
	// Flag para determinar si ejecutar en modo servidor HTTP o CLI
	serverMode := flag.Bool("server", false, "Ejecutar en modo servidor HTTP")
	port := flag.String("port", "8080", "Puerto para el servidor HTTP")
	flag.Parse()

	if *serverMode {
		startHTTPServer(*port)
	} else {
		startCLI()
	}
}

// Servidor HTTP para el frontend
func startHTTPServer(port string) {
	// Configurar CORS
	http.HandleFunc("/execute", corsMiddleware(executeCommandHandler))
	http.HandleFunc("/health", corsMiddleware(healthHandler))

	// Ruta para obtener la lista de discos
	http.HandleFunc("/disks", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Obtener discos de forma optimizada
		disks := commands.GetAllDisks()

		response := map[string]interface{}{
			"disks": disks,
			"count": len(disks),
		}

		json.NewEncoder(w).Encode(response)
	}))

	// Ruta para obtener solo discos con particiones montadas (OPTIMIZADO)
	http.HandleFunc("/disks/mounted", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Solo obtener particiones montadas (sin leer discos del sistema)
		mountedPartitions := commands.GetMountedPartitionsOnly()

		response := map[string]interface{}{
			"disks": mountedPartitions,
			"count": len(mountedPartitions),
		}

		json.NewEncoder(w).Encode(response)
	}))

	// Ruta para obtener información de la sesión actual (para que el frontend pueda sincronizar)
	http.HandleFunc("/session", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		info := commands.GetSessionInfo()
		response := map[string]interface{}{
			"success": true,
			"session": info,
		}
		json.NewEncoder(w).Encode(response)
	}))

	http.HandleFunc("/files", corsMiddleware(filesHandler))
	http.HandleFunc("/journaling", corsMiddleware(journalingHandler))
	http.HandleFunc("/journaling/repair", corsMiddleware(journalingRepairHandler))
	http.HandleFunc("/journaling/dump", corsMiddleware(journalingDumpHandler))
	http.HandleFunc("/file/read", corsMiddleware(readFileHandler))

	// Determinar dirección de enlace: primero mirar variable de entorno BACKEND_BIND_ADDR
	// Ejemplo de valor: "localhost:8080" o ":8080" para todas las interfaces
	bindAddr := os.Getenv("BACKEND_BIND_ADDR")
	if bindAddr == "" {
		// Por defecto enlazamos a localhost:PORT para mayor seguridad (no todas las interfaces)
		bindAddr = "localhost:" + port
	}

	fmt.Printf(" Servidor ExtreamFS iniciado en http://%s\n", bindAddr)
	fmt.Println(" Esperando comandos desde el frontend...")

	log.Fatal(http.ListenAndServe(bindAddr, nil))
}

// Middleware para CORS
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Configurar headers CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Responder a OPTIONS request (preflight)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// Handler para leer contenido de un archivo
func readFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PartitionID string `json:"partitionId"`
		Path        string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := map[string]interface{}{
			"success": false,
			"error":   "Error al decodificar la petición: " + err.Error(),
		}
		sendJSONResponse(w, response, http.StatusBadRequest)
		return
	}

	// Validar sesión
	session := commands.GetCurrentSession()
	if session == nil || session.User == "" {
		response := map[string]interface{}{
			"success": false,
			"error":   "Debe iniciar sesión primero",
		}
		sendJSONResponse(w, response, http.StatusUnauthorized)
		return
	}

	// Obtener partición montada (uso de helper centralizado, insensible a mayúsculas)
	mountedPartition := commands.GetMountedPartition(req.PartitionID)

	if mountedPartition == nil {
		response := map[string]interface{}{
			"success": false,
			"error":   "Partición no montada",
		}
		sendJSONResponse(w, response, http.StatusNotFound)
		return
	}

	// Leer el contenido del archivo
	content, err := commands.ReadFileByPath(mountedPartition, req.Path)
	if err != nil {
		response := map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}
		sendJSONResponse(w, response, http.StatusOK)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"content": content,
		"path":    req.Path,
	}
	sendJSONResponse(w, response, http.StatusOK)
}

// Handler para obtener el journaling
func journalingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PartitionID string `json:"partitionId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := map[string]interface{}{
			"success": false,
			"error":   "Error al decodificar la petición: " + err.Error(),
		}
		sendJSONResponse(w, response, http.StatusBadRequest)
		return
	}

	// Validar sesión
	session := commands.GetCurrentSession()
	if session == nil || session.User == "" {
		response := map[string]interface{}{
			"success": false,
			"error":   "Debe iniciar sesión primero",
		}
		sendJSONResponse(w, response, http.StatusUnauthorized)
		return
	}

	// Obtener partición montada (uso de helper centralizado, insensible a mayúsculas)
	mountedPartition := commands.GetMountedPartition(req.PartitionID)

	if mountedPartition == nil {
		response := map[string]interface{}{
			"success": false,
			"error":   "Partición no montada",
		}
		sendJSONResponse(w, response, http.StatusNotFound)
		return
	}

	// Obtener el journaling
	entries, err := commands.GetJournaling(mountedPartition)
	if err != nil {
		response := map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}
		sendJSONResponse(w, response, http.StatusOK)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"entries": entries,
		"count":   len(entries),
	}
	sendJSONResponse(w, response, http.StatusOK)
}

// Handler para reparar el journaling buscando entradas antiguas y
// consolidándolas en el layout actual.
func journalingRepairHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PartitionID string `json:"partitionId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := map[string]interface{}{
			"success": false,
			"error":   "Error al decodificar la petición: " + err.Error(),
		}
		sendJSONResponse(w, response, http.StatusBadRequest)
		return
	}

	// Validar sesión
	session := commands.GetCurrentSession()
	if session == nil || session.User == "" {
		response := map[string]interface{}{
			"success": false,
			"error":   "Debe iniciar sesión primero",
		}
		sendJSONResponse(w, response, http.StatusUnauthorized)
		return
	}

	mountedPartition := commands.GetMountedPartition(req.PartitionID)
	if mountedPartition == nil {
		response := map[string]interface{}{
			"success": false,
			"error":   "Partición no montada",
		}
		sendJSONResponse(w, response, http.StatusNotFound)
		return
	}

	count, err := commands.RepairJournal(mountedPartition)
	if err != nil {
		response := map[string]interface{}{
			"success":   false,
			"error":     err.Error(),
			"recovered": 0,
		}
		sendJSONResponse(w, response, http.StatusOK)
		return
	}

	response := map[string]interface{}{
		"success":   true,
		"recovered": count,
	}
	sendJSONResponse(w, response, http.StatusOK)
}

// Handler para volcar las regiones raw del journaling (base64) para diagnóstico
func journalingDumpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PartitionID string `json:"partitionId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := map[string]interface{}{
			"success": false,
			"error":   "Error al decodificar la petición: " + err.Error(),
		}
		sendJSONResponse(w, response, http.StatusBadRequest)
		return
	}

	// Validar sesión (mantener requisito para seguridad)
	session := commands.GetCurrentSession()
	if session == nil || session.User == "" {
		response := map[string]interface{}{
			"success": false,
			"error":   "Debe iniciar sesión primero",
		}
		sendJSONResponse(w, response, http.StatusUnauthorized)
		return
	}

	mountedPartition := commands.GetMountedPartition(req.PartitionID)
	if mountedPartition == nil {
		response := map[string]interface{}{
			"success": false,
			"error":   "Partición no montada",
		}
		sendJSONResponse(w, response, http.StatusNotFound)
		return
	}

	rawMap, err := commands.DumpJournalRegions(mountedPartition)
	if err != nil {
		response := map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}
		sendJSONResponse(w, response, http.StatusOK)
		return
	}

	// encode to base64
	candidateB64 := ""
	preferredB64 := ""
	if data, ok := rawMap["candidate1"]; ok && len(data) > 0 {
		candidateB64 = base64.StdEncoding.EncodeToString(data)
	}
	if data, ok := rawMap["preferred"]; ok && len(data) > 0 {
		preferredB64 = base64.StdEncoding.EncodeToString(data)
	}

	response := map[string]interface{}{
		"success":    true,
		"candidate1": candidateB64,
		"preferred":  preferredB64,
	}
	sendJSONResponse(w, response, http.StatusOK)
}

// Handler para ejecutar comandos
func executeCommandHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := CommandResponse{
			Success: false,
			Error:   "Error al decodificar el comando: " + err.Error(),
		}
		sendJSONResponse(w, response, http.StatusBadRequest)
		return
	}

	// Capturar la salida del comando
	output, err := executeCommandFromHTTP(req.Command)

	if err != nil {
		response := CommandResponse{
			Success: false,
			Output:  output,
			Error:   err.Error(),
		}
		sendJSONResponse(w, response, http.StatusOK)
	} else {
		response := CommandResponse{
			Success: true,
			Output:  output,
		}
		sendJSONResponse(w, response, http.StatusOK)
	}
}

// Handler para health check
func healthHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status":  "ok",
		"message": "Backend ExtreamFS funcionando correctamente",
		"version": "2.0",
	}
	sendJSONResponse(w, response, http.StatusOK)
}

// Handler para listar archivos
func filesHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("=== FILES HANDLER INICIADO ===")

	if r.Method != "POST" {
		fmt.Println("ERROR: Método no es POST")
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PartitionID string `json:"partitionId"`
		Path        string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Printf("ERROR decodificando: %v\n", err)
		response := map[string]interface{}{
			"success": false,
			"error":   "Error al decodificar la petición: " + err.Error(),
			"files":   []interface{}{},
		}
		sendJSONResponse(w, response, http.StatusBadRequest)
		return
	}

	fmt.Printf("Partición ID: %s, Path: %s\n", req.PartitionID, req.Path)

	// Validar que el usuario esté logueado
	session := commands.GetCurrentSession()
	if session == nil {
		fmt.Println("Usuario actual: <ninguno>")
	} else {
		fmt.Printf("Usuario actual: %s\n", session.User)
	}

	if session == nil || session.User == "" {
		fmt.Println("ERROR: No hay sesión activa")
		response := map[string]interface{}{
			"success": false,
			"error":   "Debe iniciar sesión primero",
			"files":   []interface{}{},
		}
		sendJSONResponse(w, response, http.StatusUnauthorized)
		return
	}

	// Obtener partición montada (uso de helper centralizado, insensible a mayúsculas)
	mountedPartition := commands.GetMountedPartition(req.PartitionID)

	if mountedPartition == nil {
		fmt.Println("ERROR: Partición no montada")
		response := map[string]interface{}{
			"success": false,
			"error":   "Partición no montada",
			"files":   []interface{}{},
		}
		sendJSONResponse(w, response, http.StatusNotFound)
		return
	}

	fmt.Println("Llamando a GetFilesList...")
	files, err := commands.GetFilesList(mountedPartition, req.Path)

	if err != nil {
		fmt.Printf("ERROR en GetFilesList: %v\n", err)
		response := map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"files":   []interface{}{},
		}
		sendJSONResponse(w, response, http.StatusOK)
		return
	}

	fmt.Printf("Archivos encontrados: %d\n", len(files))

	if files == nil {
		files = []commands.FileNode{}
	}

	response := map[string]interface{}{
		"success": true,
		"files":   files,
		"path":    req.Path,
		"count":   len(files),
	}
	sendJSONResponse(w, response, http.StatusOK)
	fmt.Println("=== FILES HANDLER COMPLETADO ===")
}

// Enviar respuesta JSON
func sendJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	// Ensure CORS headers are present for all JSON responses (extra safety)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// isComment verifica si una línea es un comentario
func isComment(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#")
}

// removeInlineComment elimina comentarios inline de una línea
func removeInlineComment(line string) string {
	// Buscar el primer # que no esté dentro de comillas
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(line); i++ {
		char := line[i]

		if !inQuotes {
			switch char {
			case '"', '\'':
				inQuotes = true
				quoteChar = char
			case '#':
				// Encontramos un comentario, devolver la parte antes del #
				return strings.TrimSpace(line[:i])
			}
		} else {
			if char == quoteChar {
				inQuotes = false
				quoteChar = 0
			}
		}
	}

	return line
}

// Ejecutar comando desde HTTP y capturar salida
func executeCommandFromHTTP(commandLine string) (string, error) {
	// Redirigir stdout para capturar la salida
	originalStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Buffer para capturar la salida
	outputChan := make(chan string)
	go func() {
		scanner := bufio.NewScanner(r)
		var output strings.Builder
		for scanner.Scan() {
			output.WriteString(scanner.Text() + "\n")
		}
		outputChan <- output.String()
	}()

	var err error

	parts := parseArguments(commandLine)

	// Ignorar líneas que contengan sólo guiones bajos o guiones (artefactos de scripts)
	trimmedLine := strings.TrimSpace(commandLine)
	if trimmedLine != "" {
		onlyUnderscores := true
		for i := 0; i < len(trimmedLine); i++ {
			c := trimmedLine[i]
			if c != '_' && c != '-' && c != ' ' && c != '\t' {
				onlyUnderscores = false
				break
			}
		}
		if onlyUnderscores {
			w.Close()
			os.Stdout = originalStdout
			return "", nil
		}
	}

	if len(parts) == 0 {
		w.Close()
		os.Stdout = originalStdout
		return "", fmt.Errorf("comando vacío")
	}

	command := strings.ToLower(parts[0])
	args := parts[1:]

	// Autoreplace de IDs desconocidos como 'A118' con la primera partición montada disponible.
	// Esto ayuda a ejecutar scripts de prueba que usan IDs antiguos o placeholders.
	for i, p := range parts {
		// Manejar formatos: -id=VAL o id=VAL dentro del comando
		if strings.HasPrefix(strings.ToLower(p), "-id=") || strings.HasPrefix(strings.ToLower(p), "id=") {
			eq := strings.Index(p, "=")
			if eq != -1 && eq < len(p)-1 {
				val := p[eq+1:]
				if commands.GetMountedPartition(val) == nil {
					// Si no existe y hay montadas, sustituir por la primera
					mountedList := commands.GetMountedPartitionsOnly()
					if len(mountedList) > 0 {
						first := mountedList[0]
						if idv, ok := first["id"].(string); ok && idv != "" {
							newToken := p[:eq+1] + idv
							parts[i] = newToken
							// Reconstruir args as necesarias
							if i == 0 {
								command = strings.ToLower(parts[0])
							}
							// Update args slice as well
							if i-1 >= 0 && i-1 < len(args) {
								args[i-1] = newToken
							}
							fmt.Printf("Nota: ID '%s' no encontrado. Usando '%s' en su lugar.\n", val, idv)
						}
					}
				}
			}
		}
	}

	// Permitir que comandos ejecutados desde el endpoint HTTP se ejecuten
	// sin requerir sesión activa (la GUI pedirá sesión sólo para visualizador).
	commands.SetAllowCommandsWithoutSession(true)
	defer commands.SetAllowCommandsWithoutSession(false)

	err = executeCommand(command, args, commandLine)

	// Restaurar stdout y obtener salida
	w.Close()
	os.Stdout = originalStdout
	output := <-outputChan

	return output, err
}

// Función para ejecutar comandos
func executeCommand(command string, args []string, fullLine string) error {
	switch command {
	case "mkdisk":
		mkdiskCmd := flag.NewFlagSet("mkdisk", flag.ContinueOnError)
		size := mkdiskCmd.Int("size", 0, "Tamaño del disco")
		unit := mkdiskCmd.String("unit", "m", "Unidad del tamaño (K o M).")
		fit := mkdiskCmd.String("fit", "ff", "Tipo de ajuste (BF, FF, WF).")
		path := mkdiskCmd.String("path", "", "Ruta del disco a crear.")

		if err := mkdiskCmd.Parse(args); err != nil {
			return err
		}

		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para mkdisk")
		}
		if *size <= 0 {
			return fmt.Errorf("el parámetro -size es obligatorio y debe ser positivo")
		}

		commands.ExecuteMkdisk(*size, *unit, *fit, *path)

	case "rmdisk":
		rmdiskCmd := flag.NewFlagSet("rmdisk", flag.ContinueOnError)
		path := rmdiskCmd.String("path", "", "Ruta del disco a eliminar.")

		if err := rmdiskCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para rmdisk")
		}

		commands.ExecuteRmdisk(*path)

	case "fdisk":
		fdiskCmd := flag.NewFlagSet("fdisk", flag.ContinueOnError)
		size := fdiskCmd.Int64("size", 0, "Tamaño de la partición")
		unit := fdiskCmd.String("unit", "m", "Unidad del tamaño (K o M).")
		path := fdiskCmd.String("path", "", "Ruta del disco donde se encuentra la partición.")
		tipo := fdiskCmd.String("type", "primaria", "Tipo de partición (primaria o extendida).")
		fit := fdiskCmd.String("fit", "ff", "Tipo de ajuste (BF, FF, WF).")
		name := fdiskCmd.String("name", "", "Nombre de la partición.")
		delete := fdiskCmd.String("delete", "", "Tipo de eliminación (fast o full).")
		add := fdiskCmd.Int64("add", 0, "Cantidad de espacio a agregar o quitar.")

		if err := fdiskCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para fdisk")
		}

		// Solo validar -size si NO es delete ni add
		if *delete == "" && *add == 0 {
			if *size <= 0 {
				return fmt.Errorf("el parámetro -size es obligatorio y debe ser positivo para crear particiones")
			}
		}

		commands.ExecuteFdisk(*size, *unit, *path, *tipo, *fit, *name, *delete, *add)

	case "mount":
		mountCmd := flag.NewFlagSet("mount", flag.ContinueOnError)
		path := mountCmd.String("path", "", "Ruta del disco")
		name := mountCmd.String("name", "", "Nombre de la partición")

		if err := mountCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para mount")
		}
		if *name == "" {
			return fmt.Errorf("el parámetro -name es obligatorio para mount")
		}

		commands.ExecuteMount(*path, *name)

	case "mounted":
		commands.ExecuteMounted()

	case "unmount":
		unmountCmd := flag.NewFlagSet("unmount", flag.ContinueOnError)
		id := unmountCmd.String("id", "", "ID de la partición montada")

		if err := unmountCmd.Parse(args); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("el parámetro -id es obligatorio para unmount")
		}

		commands.ExecuteUnmount(*id)

	case "mkfs":
		mkfsCmd := flag.NewFlagSet("mkfs", flag.ContinueOnError)
		id := mkfsCmd.String("id", "", "ID de la partición montada")
		formatType := mkfsCmd.String("type", "full", "Tipo de formateo (full)")
		fs := mkfsCmd.String("fs", "2fs", "Tipo de sistema de archivos (2fs o 3fs)")

		if err := mkfsCmd.Parse(args); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("el parámetro -id es obligatorio para mkfs")
		}

		commands.ExecuteMkfs(*id, *formatType, *fs)

	case "login":
		loginCmd := flag.NewFlagSet("login", flag.ContinueOnError)
		user := loginCmd.String("user", "", "Nombre de usuario")
		pass := loginCmd.String("pass", "", "Contraseña del usuario")
		id := loginCmd.String("id", "", "ID de la partición montada")

		if err := loginCmd.Parse(args); err != nil {
			return err
		}
		if *user == "" {
			return fmt.Errorf("el parámetro -user es obligatorio para login")
		}
		if *pass == "" {
			return fmt.Errorf("el parámetro -pass es obligatorio para login")
		}

		commands.ExecuteLogin(*user, *pass, *id)

	case "logout":
		commands.ExecuteLogout()

	case "cat":
		catCmd := flag.NewFlagSet("cat", flag.ContinueOnError)
		files := make(map[string]*string)
		for i := 1; i <= 10; i++ {
			flagName := fmt.Sprintf("file%d", i)
			files[flagName] = catCmd.String(flagName, "", fmt.Sprintf("Archivo %d a mostrar", i))
		}

		if err := catCmd.Parse(args); err != nil {
			return err
		}

		fileMap := make(map[string]string)
		for flagName, flagValue := range files {
			if *flagValue != "" {
				fileMap[flagName] = *flagValue
			}
		}

		if len(fileMap) == 0 {
			return fmt.Errorf("debe especificar al menos un archivo con -file1")
		}

		commands.ExecuteCat(fileMap)

	case "mkgrp":
		mkgrpCmd := flag.NewFlagSet("mkgrp", flag.ContinueOnError)
		groupName := mkgrpCmd.String("name", "", "Nombre del grupo a crear")

		if err := mkgrpCmd.Parse(args); err != nil {
			return err
		}
		if *groupName == "" {
			return fmt.Errorf("el parámetro -name es obligatorio para mkgrp")
		}

		commands.ExecuteMkgrp(*groupName)

	case "rmgrp":
		rmgrpCmd := flag.NewFlagSet("rmgrp", flag.ContinueOnError)
		groupName := rmgrpCmd.String("name", "", "Nombre del grupo a eliminar")

		if err := rmgrpCmd.Parse(args); err != nil {
			return err
		}
		if *groupName == "" {
			return fmt.Errorf("el parámetro -name es obligatorio para rmgrp")
		}

		commands.ExecuteRmgrp(*groupName)

	case "mkusr":
		mkusrCmd := flag.NewFlagSet("mkusr", flag.ContinueOnError)
		username := mkusrCmd.String("user", "", "Nombre del usuario a crear")
		password := mkusrCmd.String("pass", "", "Contraseña del usuario")
		groupName := mkusrCmd.String("grp", "", "Grupo al que pertenece el usuario")

		if err := mkusrCmd.Parse(args); err != nil {
			return err
		}
		if *username == "" || *password == "" || *groupName == "" {
			return fmt.Errorf("los parámetros -user, -pass y -grp son obligatorios para mkusr")
		}

		commands.ExecuteMkusr(*username, *password, *groupName)

	case "rmusr":
		rmusrCmd := flag.NewFlagSet("rmusr", flag.ContinueOnError)
		username := rmusrCmd.String("user", "", "Nombre del usuario a eliminar")

		if err := rmusrCmd.Parse(args); err != nil {
			return err
		}
		if *username == "" {
			return fmt.Errorf("el parámetro -user es obligatorio para rmusr")
		}

		commands.ExecuteRmusr(*username)

	case "chgrp":
		chgrpCmd := flag.NewFlagSet("chgrp", flag.ContinueOnError)
		username := chgrpCmd.String("user", "", "Nombre del usuario")
		groupName := chgrpCmd.String("grp", "", "Nuevo grupo del usuario")

		if err := chgrpCmd.Parse(args); err != nil {
			return err
		}
		if *username == "" || *groupName == "" {
			return fmt.Errorf("los parámetros -user y -grp son obligatorios para chgrp")
		}

		commands.ExecuteChgrp(*username, *groupName)

	case "mkfile":
		mkfileCmd := flag.NewFlagSet("mkfile", flag.ContinueOnError)
		path := mkfileCmd.String("path", "", "Ruta del archivo a crear")
		recursive := mkfileCmd.Bool("r", false, "Crear directorios padre si no existen")
		size := mkfileCmd.Int("size", 0, "Tamaño del archivo en bytes")
		cont := mkfileCmd.String("cont", "", "Archivo del sistema con contenido")

		if err := mkfileCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para mkfile")
		}

		commands.ExecuteMkfile(*path, *recursive, *size, *cont)

	case "mkdir":
		mkdirArgs := parseArguments(fullLine)[1:]
		mkdirCmd := flag.NewFlagSet("mkdir", flag.ContinueOnError)
		path := mkdirCmd.String("path", "", "Ruta del directorio a crear")
		parents := mkdirCmd.Bool("p", false, "Crear directorios padre si no existen")

		if err := mkdirCmd.Parse(mkdirArgs); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para mkdir")
		}

		commands.ExecuteMkdir(*path, *parents)

	case "rep":
		repCmd := flag.NewFlagSet("rep", flag.ContinueOnError)
		name := repCmd.String("name", "", "Nombre del reporte (mbr, disk, inode, block, bm_inode, bm_block, tree, sb, file, ls)")
		path := repCmd.String("path", "", "Ruta donde guardar el reporte")
		id := repCmd.String("id", "", "ID de la partición montada (opcional si se usa -disk)")
		disk := repCmd.String("disk", "", "Ruta al archivo de disco (.mia) para generar reportes sin montar la partición (opcional)")
		pathFileLs := repCmd.String("path_file_ls", "", "Ruta del archivo o carpeta (para reportes file y ls)")

		if err := repCmd.Parse(args); err != nil {
			return err
		}
		if *name == "" || *path == "" {
			return fmt.Errorf("los parámetros -name y -path son obligatorios para rep")
		}
		// Either -id or -disk can be used. If both are empty, we want to fail.
		if *id == "" && *disk == "" {
			return fmt.Errorf("al menos uno de los parámetros -id o -disk es obligatorio para rep cuando no hay particiones montadas")
		}

		commands.ExecuteRep(*name, *path, *id, *pathFileLs, *disk)

	case "remove":
		removeCmd := flag.NewFlagSet("remove", flag.ContinueOnError)
		path := removeCmd.String("path", "", "Ruta del archivo o carpeta a eliminar")

		if err := removeCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para remove")
		}
		commands.ExecuteRemove(*path)

	case "edit":
		editCmd := flag.NewFlagSet("edit", flag.ContinueOnError)
		path := editCmd.String("path", "", "Ruta del archivo a editar")
		contenido := editCmd.String("contenido", "", "Archivo del sistema con el nuevo contenido")

		if err := editCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para edit")
		}
		if *contenido == "" {
			return fmt.Errorf("el parámetro -contenido es obligatorio para edit")
		}

		commands.ExecuteEdit(*path, *contenido)

	case "rename":
		renameCmd := flag.NewFlagSet("rename", flag.ContinueOnError)
		path := renameCmd.String("path", "", "Ruta del archivo o carpeta a renombrar")
		name := renameCmd.String("name", "", "Nuevo nombre del archivo o carpeta")

		if err := renameCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para rename")
		}
		if *name == "" {
			return fmt.Errorf("el parámetro -name es obligatorio para rename")
		}

		commands.ExecuteRename(*path, *name)

	case "copy":
		copyCmd := flag.NewFlagSet("copy", flag.ContinueOnError)
		path := copyCmd.String("path", "", "Ruta del archivo o carpeta origen")
		destino := copyCmd.String("destino", "", "Ruta del archivo o carpeta destino")

		if err := copyCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para copy")
		}
		if *destino == "" {
			return fmt.Errorf("el parámetro -destino es obligatorio para copy")
		}

		commands.ExecuteCopy(*path, *destino)

	case "move":
		moveCmd := flag.NewFlagSet("move", flag.ContinueOnError)
		path := moveCmd.String("path", "", "Ruta del archivo o carpeta origen")
		destino := moveCmd.String("destino", "", "Ruta del archivo o carpeta destino")

		if err := moveCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para move")
		}
		if *destino == "" {
			return fmt.Errorf("el parámetro -destino es obligatorio para move")
		}

		commands.ExecuteMove(*path, *destino)

	case "find":
		findCmd := flag.NewFlagSet("find", flag.ContinueOnError)
		path := findCmd.String("path", "", "Ruta donde buscar")
		name := findCmd.String("name", "", "Patrón de nombre a buscar (regex)")

		if err := findCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para find")
		}
		if *name == "" {
			return fmt.Errorf("el parámetro -name es obligatorio para find")
		}

		commands.ExecuteFind(*path, *name)

	case "chown":
		chownCmd := flag.NewFlagSet("chown", flag.ContinueOnError)
		path := chownCmd.String("path", "", "Ruta del archivo o carpeta")
		recursive := chownCmd.Bool("r", false, "Cambiar propietario de forma recursiva")
		usuario := chownCmd.String("usuario", "", "Nuevo propietario")

		if err := chownCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para chown")
		}
		if *usuario == "" {
			return fmt.Errorf("el parámetro -usuario es obligatorio para chown")
		}

		commands.ExecuteChown(*path, *recursive, *usuario)

	case "chmod":
		chmodCmd := flag.NewFlagSet("chmod", flag.ContinueOnError)
		path := chmodCmd.String("path", "", "Ruta del archivo o carpeta")
		recursive := chmodCmd.Bool("r", false, "Cambiar permisos de forma recursiva")
		ugo := chmodCmd.String("ugo", "", "Nuevos permisos (rwxrwxrwx)")

		if err := chmodCmd.Parse(args); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("el parámetro -path es obligatorio para chmod")
		}
		if *ugo == "" {
			return fmt.Errorf("el parámetro -ugo es obligatorio para chmod")
		}

		commands.ExecuteChmod(*path, *recursive, *ugo)

	case "recovery":
		recoveryCmd := flag.NewFlagSet("recovery", flag.ContinueOnError)
		id := recoveryCmd.String("id", "", "ID de la partición montada")

		if err := recoveryCmd.Parse(args); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("el parámetro -id es obligatorio para recovery")
		}

		commands.ExecuteRecovery(*id)

	case "loss":
		lossCmd := flag.NewFlagSet("loss", flag.ContinueOnError)
		id := lossCmd.String("id", "", "ID de la partición montada")

		if err := lossCmd.Parse(args); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("el parámetro -id es obligatorio para loss")
		}

		commands.ExecuteLoss(*id)

	case "journaling":
		journalCmd := flag.NewFlagSet("journaling", flag.ContinueOnError)
		id := journalCmd.String("id", "", "ID de la partición montada")

		if err := journalCmd.Parse(args); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("el parámetro -id es obligatorio para journaling")
		}

		// Buscar partición montada por ID (usa la función centralizada que ahora es insensible a mayúsculas)
		mounted := commands.GetMountedPartition(*id)
		if mounted == nil {
			return fmt.Errorf("no se encontró una partición montada con ID '%s'", *id)
		}

		entries, err := commands.GetJournaling(mounted)
		if err != nil {
			return fmt.Errorf("error al obtener journaling: %v", err)
		}

		fmt.Printf("Journaling entries (count=%d):\n", len(entries))
		for idx, e := range entries {
			fmt.Printf("[%d] %s %s %s\n", idx, e.Timestamp, e.Operation, e.Path)
			if e.Content != "" {
				fmt.Printf("    Content: %s\n", e.Content)
			}
		}

	default:
		return fmt.Errorf("comando no reconocido: %s", command)
	}

	return nil
}

func startCLI() {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("╰─➤ ")
		if !scanner.Scan() {
			break
		}

		line := scanner.Text()

		if strings.ToLower(line) == "exit" {
			fmt.Println("Exiting...")
			break
		}

		// Verificar si es un comentario
		if isComment(line) {
			fmt.Println("💬 Comentario ignorado")
			continue
		}

		// Remover comentarios inline
		line = removeInlineComment(line)

		// Si después de remover comentarios la línea está vacía, continuar
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := parseArguments(line)

		if len(parts) == 0 {
			continue
		}

		command := strings.ToLower(parts[0])
		args := parts[1:]

		if err := executeCommand(command, args, line); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading input:", err)
	}
}

func parseArguments(line string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(line); i++ {
		char := line[i]

		if !inQuotes {
			switch char {
			case '"', '\'':
				inQuotes = true
				quoteChar = char
				current.WriteByte(char) // Incluir la comilla
			case ' ', '\t':
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			default:
				current.WriteByte(char)
			}
		} else {
			current.WriteByte(char)
			if char == quoteChar {
				inQuotes = false
				quoteChar = 0
			}
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}
