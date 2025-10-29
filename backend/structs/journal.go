package structs

// Information - Contiene la información de una operación realizada en el sistema
type Information struct {
    IOperation [10]byte // Contiene la operación que se realizó
    IPath      [32]byte // Contiene el path donde se realizó la operación
    IContent   [64]byte // Contiene todo el contenido (si es un archivo)
    IDate      float32  // Contiene la fecha en la que se hizo la operación
}

// Journal - Bitácora de todas las acciones realizadas en el sistema de archivos
type Journal struct {
    JCount   int32       // Lleva el conteo del journal que es
    JContent Information // Contiene toda la información de la acción que se hizo
}

// JournalRecovery - Estructura extendida para recuperación del sistema de archivos EXT3
type JournalRecovery struct {
    Journal_ultimo_montaje   int64      // Timestamp del último montaje
    Journal_inodos_libres    int64      // Cantidad de inodos libres
    Journal_bloques_libres   int64      // Cantidad de bloques libres
    Journal_bm_inodos        [500]byte  // Copia del bitmap de inodos
    Journal_bm_bloques       [500]byte  // Copia del bitmap de bloques
    Journal_inodos           [20000]byte // Copia del área de inodos
    Journal_bloques          [50000]byte // Copia del área de bloques
}