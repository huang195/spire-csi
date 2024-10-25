package cert

import(
    "fmt"
    "io/ioutil"

    "crypto/x509"
    "encoding/pem"
    "time"
)

func GetCertificateExpirationTime(certFile string) (time.Time, error) {
    // Read the certificate file
    certPEM, err := ioutil.ReadFile(certFile)
    if err != nil {
        return time.Time{}, err
    }

    // Decode PEM data
    block, _ := pem.Decode(certPEM)
    if block == nil || block.Type != "CERTIFICATE" {
        return time.Time{}, fmt.Errorf("failed to decode PEM block containing the certificate")
    }

    // Parse the certificate
    cert, err := x509.ParseCertificate(block.Bytes)
    if err != nil {
        return time.Time{}, err
    }

    return cert.NotAfter, nil
}
