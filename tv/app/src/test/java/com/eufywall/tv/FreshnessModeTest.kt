package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class FreshnessModeTest {
    @Test fun versionOneWithoutModeKeepsEnforcementContract() {
        assertTrue(FreshnessCapability(500,"http://bridge/api/clock").enforces)
    }
    @Test fun capabilityRevisionTwoRequiresAnExplicitKnownMode() {
        assertEquals("observe",freshnessMode(2,"observe"))
        assertEquals("enforce",freshnessMode(2,"enforce"))
        assertNull(freshnessMode(2,null))
        assertNull(freshnessMode(2,"other"))
        assertNull(freshnessMode(3,"observe"))
    }
    @Test fun revisionOnePreservesLegacyEnforcementOnly() {
        assertEquals("enforce",freshnessMode(1,null))
        assertEquals("enforce",freshnessMode(1,"enforce"))
        assertNull(freshnessMode(1,"observe"))
    }
    @Test fun observeModeDoesNotEnableBlanking() {
        val observe=FreshnessCapability(500,"http://bridge/api/clock","observe")
        assertFalse(observe.enforces)
        assertEquals(500,observe.maxAgeMs)
    }
}
