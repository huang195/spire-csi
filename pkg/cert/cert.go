package cert

import(
    "crypto/x509"
    "encoding/pem"
    "time"
)

func ParseCertificate(certFile string) (time.Time, error) {
    // Read the certificate file
    certPEM, err := ioutil.ReadFile(certFile)
    if err != nil {
        return nil, err
    }

    // Decode PEM data
    block, _ := pem.Decode(certPEM)
    if block == nil || block.Type != "CERTIFICATE" {
        return nil, fmt.Errorf("failed to decode PEM block containing the certificate")
    }

    // Parse the certificate
    cert, err := x509.ParseCertificate(block.Bytes)
    if err != nil {
        return nil, err
    }

    return cert.NotAfter, nil
}
