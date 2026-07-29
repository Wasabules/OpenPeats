//go:build !linux && !windows

package peats

import "time"

// Plateformes sans transport RFCOMM implémenté.
//
// macOS — réalisable, mais pas encore fait. Le RFCOMM y passe par le framework
// IOBluetooth (Objective-C, API asynchrone à base de délégués) : il faut un
// pont cgo qui ouvre un IOBluetoothRFCOMMChannel et convertit les rappels du
// délégué en lectures bloquantes. Voir IOBluetoothDevice
// -performSDPQuery: puis -openRFCOMMChannelSync:withChannelID:delegate:.
// Contrairement à Linux et Windows, il n'existe pas d'API socket : le code ne
// peut pas être écrit en miroir des deux autres transports.
//
// iOS — hors d'atteinte. Le profil SPP y est réservé aux accessoires certifiés
// MFi, via le framework ExternalAccessory. Ces écouteurs ne le sont pas.
// Aucun contournement n'existe côté application.
//
// Android — le noyau est Linux et gère AF_BLUETOOTH, mais SELinux et le modèle
// de permissions bloquent les sockets Bluetooth bruts depuis le NDK. Le chemin
// pris en charge est android.bluetooth.BluetoothSocket, côté Java/Kotlin.

func init() {
	dialRFCOMM = func([6]byte, uint8, time.Duration) (rfcommConn, error) {
		return nil, ErrUnsupported
	}
}

func isTimeout(error) bool { return false }
