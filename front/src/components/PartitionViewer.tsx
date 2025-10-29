import React from 'react';
import '../App.css';

interface Partition {
  name: string;
  id: string;
  size: number;
  type: string;
  isMounted: boolean;
  status: string;
}

interface Disk {
  path: string;
  size: number;
  unit: string;
  fit: string;
  partitions: Partition[];
}

interface PartitionViewerProps {
  disk: Disk;
  session: {
    partitionId: string;
    username: string;
    isLoggedIn: boolean;
    isRoot: boolean;
  };
  onSelectPartition: (partition: Partition) => void;
  hasAccessToPartition: (partition: Partition) => boolean;
  onBack: () => void;
}

const PartitionViewer: React.FC<PartitionViewerProps> = ({
  disk,
  session,
  onSelectPartition,
  hasAccessToPartition,
}) => {
  const handlePartitionClick = (partition: Partition) => {
    if (hasAccessToPartition(partition)) {
      onSelectPartition(partition);
    }
  };

  const getPartitionIcon = (type: string) => {
    switch (type) {
      case 'Extendida':
        return '📦';
      case 'Lógica':
        return '📄';
      default:
        return '📁';
    }
  };

  const getAccessMessage = (partition: Partition) => {
    if (!partition.isMounted) {
      return '🔒 Partición no montada';
    }
    if (session.isRoot) {
      return '🔑 Acceso root';
    }
    if (partition.id === session.partitionId) {
      return '✅ Tienes acceso';
    }
    return '🔒 Sin permisos de acceso';
  };

  return (
    <div className="partition-viewer">
      <div className="disk-info-card">
        <div className="disk-info-header">
          <h3>📀 Información del Disco</h3>
        </div>
        <div className="disk-info-content">
          <div className="info-row">
            <span className="info-label">Ruta:</span>
            <span className="info-value">{disk.path}</span>
          </div>
          <div className="info-row">
            <span className="info-label">Tamaño:</span>
            <span className="info-value">{disk.size} {disk.unit}</span>
          </div>
          <div className="info-row">
            <span className="info-label">Fit:</span>
            <span className="info-value">{disk.fit}</span>
          </div>
          <div className="info-row">
            <span className="info-label">Particiones:</span>
            <span className="info-value">{disk.partitions.length}</span>
          </div>
        </div>
      </div>

      <div className="partitions-section">
        <h3>📁 Particiones del Disco</h3>
        
        {disk.partitions.length === 0 ? (
          <div className="empty-state">
            <div className="empty-icon">📁</div>
            <p className='color-black'>Este disco no tiene particiones</p>
          </div>
        ) : (
          <div className="partitions-grid">
            {disk.partitions.map((partition, index) => {
              const hasAccess = hasAccessToPartition(partition);
              const isCurrentPartition = partition.id === session.partitionId;

              return (
                <div
                  key={index}
                  className={`partition-card ${
                    hasAccess ? 'accessible' : 'restricted'
                  } ${isCurrentPartition ? 'current' : ''}`}
                  onClick={() => handlePartitionClick(partition)}
                  style={{ cursor: hasAccess ? 'pointer' : 'not-allowed' }}
                >
                  {!hasAccess && <div className="restricted-overlay" />}
                  
                  <div className="partition-header">
                    <span className="partition-icon-large">
                      {getPartitionIcon(partition.type)}
                    </span>
                    <div className="partition-badges">
                      {isCurrentPartition && (
                        <span className="badge badge-primary">Sesión Actual</span>
                      )}
                      {partition.isMounted && (
                        <span className="badge badge-success">Montada</span>
                      )}
                      {!partition.isMounted && (
                        <span className="badge badge-secondary">No Montada</span>
                      )}
                    </div>
                  </div>

                  <div className="partition-body">
                    <h4 className="partition-name">{partition.name}</h4>
                    {partition.id && (
                      <p className="partition-id">ID: {partition.id}</p>
                    )}
                    
                    <div className="partition-details">
                      <div className="detail-item">
                        <span className="detail-label">Tipo:</span>
                        <span className="detail-value">{partition.type}</span>
                      </div>
                      <div className="detail-item">
                        <span className="detail-label">Tamaño:</span>
                        <span className="detail-value">
                          {(partition.size / (1024 * 1024)).toFixed(2)} MB
                        </span>
                      </div>
                      <div className="detail-item">
                        <span className="detail-label">Estado:</span>
                        <span className="detail-value">{partition.status}</span>
                      </div>
                    </div>
                  </div>

                  <div className="partition-footer">
                    <div className="access-status">
                      {getAccessMessage(partition)}
                    </div>
                    {hasAccess && (
                      <div className="partition-action">
                        <button className="btn-explore">
                          Explorar →
                        </button>
                      </div>
                    )}
                  </div>

                  {!hasAccess && (
                    <div className="lock-icon-overlay">
                      🔒
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};

export default PartitionViewer;